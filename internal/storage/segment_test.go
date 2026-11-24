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
func mustOpen(t *testing.T, path string) *segment {
	t.Helper()
	s, err := openSegment(path)
	if err != nil {
		t.Fatalf("openSegment: unexpected error: %v", err)
	}
	t.Cleanup(func() { s.file.Close() })
	return s
}

// mustAppend appends payload and returns its position, failing the test on error.
func mustAppend(t *testing.T, s *segment, payload string) int64 {
	t.Helper()
	pos, err := s.append([]byte(payload))
	if err != nil {
		t.Fatalf("append(%q): unexpected error: %v", payload, err)
	}
	return pos
}

// readPayloads reads every record in the file at path.
func readPayloads(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
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

func TestOpenSegmentNew(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "test.log"))

	if s.size != 0 {
		t.Errorf("size = %d, want 0", s.size)
	}
}

func TestAppendPositions(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "test.log"))

	if pos := mustAppend(t, s, "hi"); pos != 0 {
		t.Errorf("first position = %d, want 0", pos)
	}
	if pos := mustAppend(t, s, "hello"); pos != 10 {
		t.Errorf("second position = %d, want 10", pos)
	}
	if s.size != 23 {
		t.Errorf("size = %d, want 23", s.size)
	}
}

func TestAppendWritesRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	s := mustOpen(t, path)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")

	want := []string{"hi", "hello"}
	if got := readPayloads(t, path); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestReopenSegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	s := mustOpen(t, path)
	mustAppend(t, s, "hi")
	mustAppend(t, s, "hello")
	if err := s.file.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	s = mustOpen(t, path)
	if s.size != 23 {
		t.Errorf("size after reopen = %d, want 23", s.size)
	}
	if pos := mustAppend(t, s, "again"); pos != 23 {
		t.Errorf("position after reopen = %d, want 23", pos)
	}

	want := []string{"hi", "hello", "again"}
	if got := readPayloads(t, path); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestAppendTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	s := mustOpen(t, path)
	mustAppend(t, s, "hi")

	tooLarge := make([]byte, 1<<20+1) // one byte over record's 1 MiB limit
	if _, err := s.append(tooLarge); !errors.Is(err, record.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	if s.size != 10 {
		t.Errorf("size = %d, want 10", s.size)
	}

	want := []string{"hi"}
	if got := readPayloads(t, path); !slices.Equal(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
}

func TestOpenSegmentMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "test.log")

	if _, err := openSegment(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestAppendFailedWrite(t *testing.T) {
	s := mustOpen(t, filepath.Join(t.TempDir(), "test.log"))
	mustAppend(t, s, "hi")
	s.file.Close() // writing to a closed file always fails

	if _, err := s.append([]byte("hello")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("err = %v, want os.ErrClosed", err)
	}
	if s.size != 10 {
		t.Errorf("size = %d, want 10", s.size)
	}
}
