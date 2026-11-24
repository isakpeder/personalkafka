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
	t.Cleanup(func() { s.file.Close() })
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
	if err := s.file.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
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
