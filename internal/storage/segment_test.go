package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isakpeder/personalkafka/internal/record"
)

// mustOpen opens a segment, failing the test on error. The file is closed when the test ends.
func mustOpen(t *testing.T, dir string, baseOffset int64) *segment {
	t.Helper()
	s, err := openSegment(dir, baseOffset)
	if err != nil {
		t.Fatalf("openSegment: unexpected error: %v", err)
	}
	t.Cleanup(func() { s.close() })
	return s
}

// mustAppend appends payload and returns its offset and position, failing the test on error.
func mustAppend(t *testing.T, s *segment, payload string) (offset, pos int64) {
	t.Helper()
	offset, pos, err := s.append([]byte(payload))
	if err != nil {
		t.Fatalf("append(%q): unexpected error: %v", payload, err)
	}
	return offset, pos
}

// readPayloads reads every record in s's file.
func readPayloads(t *testing.T, s *segment) []string {
	t.Helper()
	f, err := os.Open(s.file.Name())
	if err != nil {
		t.Fatalf("os.Open: unexpected error: %v", err)
	}
	defer f.Close()

	var payloads []string
	for {
		payload, err := record.Read(f)
		if errors.Is(err, io.EOF) {
			return payloads
		}
		if err != nil {
			t.Fatalf("record.Read: unexpected error: %v", err)
		}
		payloads = append(payloads, string(payload))
	}
}

func TestSegmentName(t *testing.T) {
	want := "00000000000000001000.log"
	if got := segmentName(1000); got != want {
		t.Errorf("segmentName(1000) = %q, want %q", got, want)
	}

	baseOffset, err := parseSegmentName(want)
	if err != nil {
		t.Fatalf("parseSegmentName: unexpected error: %v", err)
	}
	if baseOffset != 1000 {
		t.Errorf("parseSegmentName = %d, want 1000", baseOffset)
	}
}

func TestParseSegmentNameBad(t *testing.T) {
	names := []string{
		"00000000000000001000",     // no .log
		"00000000000000001000.txt", // wrong ending
		"abc.log",                  // not a number
		"5.log",                    // not padded
		"-0000000000000001000.log", // negative
		"99999999999999999999.log", // too big for an int64
	}
	for _, name := range names {
		if baseOffset, err := parseSegmentName(name); err == nil {
			t.Errorf("parseSegmentName(%q) = %d, want an error", name, baseOffset)
		}
	}
}

func TestOpenSegmentNew(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)

	if s.size != 0 {
		t.Errorf("size = %d, want 0", s.size)
	}
	if s.baseOffset != 1000 || s.nextOffset != 1000 {
		t.Errorf("baseOffset, nextOffset = %d, %d, want 1000, 1000", s.baseOffset, s.nextOffset)
	}
	if name := filepath.Base(s.file.Name()); name != segmentName(1000) {
		t.Errorf("file name = %q, want %q", name, segmentName(1000))
	}
}

func TestAppendOffsetsAndPositions(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)

	if offset, pos := mustAppend(t, s, "hi"); offset != 1000 || pos != 0 {
		t.Errorf("first append = %d, %d, want 1000, 0", offset, pos)
	}
	if offset, pos := mustAppend(t, s, "hello"); offset != 1001 || pos != 10 {
		t.Errorf("second append = %d, %d, want 1001, 10", offset, pos)
	}
	if s.size != 23 || s.nextOffset != 1002 {
		t.Errorf("size, nextOffset = %d, %d, want 23, 1002", s.size, s.nextOffset)
	}
}

func TestAppendWritesRecords(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 0)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")

	want := []string{"hi", "hello"}
	if got := readPayloads(t, s); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestReopenSegment(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir, 1000)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")
	if err := s.close(); err != nil {
		t.Fatalf("close: unexpected error: %v", err)
	}

	s = mustOpen(t, dir, 1000)
	if s.size != 23 || s.nextOffset != 1002 {
		t.Errorf("size, nextOffset after reopen = %d, %d, want 23, 1002", s.size, s.nextOffset)
	}
	if offset, pos := mustAppend(t, s, "again"); offset != 1002 || pos != 23 {
		t.Errorf("append after reopen = %d, %d, want 1002, 23", offset, pos)
	}

	want := []string{"hi", "hello", "again"}
	if got := readPayloads(t, s); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestOpenSegmentCutOff(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir, 1000)
	mustAppend(t, s, "hi")
	if err := s.file.Truncate(9); err != nil { // "hi" record is 10 bytes
		t.Fatalf("Truncate: unexpected error: %v", err)
	}

	if _, err := openSegment(dir, 1000); !errors.Is(err, record.ErrIncomplete) {
		t.Errorf("err = %v, want ErrIncomplete", err)
	}
}

func TestOpenSegmentMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	if _, err := openSegment(dir, 0); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestAppendTooLarge(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")

	tooLarge := make([]byte, 1<<20+1) // one byte over record's 1 MiB limit
	if _, _, err := s.append(tooLarge); !errors.Is(err, record.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	if s.size != 10 || s.nextOffset != 1001 {
		t.Errorf("size, nextOffset = %d, %d, want 10, 1001", s.size, s.nextOffset)
	}

	want := []string{"hi"}
	if got := readPayloads(t, s); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestAppendFailedWrite(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")
	s.file.Close() // writing to a closed file always fails

	if _, _, err := s.append([]byte("hello")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("err = %v, want os.ErrClosed", err)
	}
	if s.size != 10 || s.nextOffset != 1001 {
		t.Errorf("size, nextOffset = %d, %d, want 10, 1001", s.size, s.nextOffset)
	}
}

// mustReadAt reads the record at pos and returns its payload, failing the test on error.
func mustReadAt(t *testing.T, s *segment, pos int64) string {
	t.Helper()
	payload, err := s.readAt(pos)
	if err != nil {
		t.Fatalf("readAt(%d): unexpected error: %v", pos, err)
	}
	return string(payload)
}

func TestReadAtAnyOrder(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")

	if got := mustReadAt(t, s, 10); got != "hello" {
		t.Errorf("readAt(10) = %q, want %q", got, "hello")
	}
	if got := mustReadAt(t, s, 0); got != "hi" {
		t.Errorf("readAt(0) = %q, want %q", got, "hi")
	}
}

func TestAppendAfterReadAt(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")
	mustReadAt(t, s, 0)

	if _, pos := mustAppend(t, s, "again"); pos != 23 {
		t.Errorf("position after read = %d, want 23", pos)
	}
	if got := mustReadAt(t, s, 23); got != "again" {
		t.Errorf("readAt(23) = %q, want %q", got, "again")
	}
}

func TestReadAtEnd(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")

	if _, err := s.readAt(10); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestReadAtOutside(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")

	for _, pos := range []int64{-1, 11} {
		if _, err := s.readAt(pos); err == nil || errors.Is(err, io.EOF) {
			t.Errorf("readAt(%d): err = %v, want an out of range error", pos, err)
		}
	}
}

func TestReadAtCorrupt(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")

	// s.file can only write at the end, so damage the record through a second open file.
	f, err := os.OpenFile(s.file.Name(), os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("os.OpenFile: unexpected error: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteAt([]byte{'X'}, 8); err != nil { // "hi" -> "Xi"
		t.Fatalf("WriteAt: unexpected error: %v", err)
	}

	if _, err := s.readAt(0); !errors.Is(err, record.ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestReadAtIgnoresJunk(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")
	if _, err := s.file.Write([]byte("junk")); err != nil { // skips append, so s.size stays 10
		t.Fatalf("Write: unexpected error: %v", err)
	}

	if _, err := s.readAt(10); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestReadAtKeepsFilePosition(t *testing.T) {
	s := mustOpen(t, t.TempDir(), 1000)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")

	before, err := s.file.Seek(0, io.SeekCurrent) // move by 0 = just report the position
	if err != nil {
		t.Fatalf("Seek: unexpected error: %v", err)
	}
	mustReadAt(t, s, 0)
	after, err := s.file.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek: unexpected error: %v", err)
	}
	if before != after {
		t.Errorf("file position moved from %d to %d", before, after)
	}
}
