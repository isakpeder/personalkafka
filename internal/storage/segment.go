package storage

import (
	"errors"
	"os"

	"github.com/isakpeder/personalkafka/internal/record"
)

// segment is one file on disk holding records back to back.
type segment struct {
	file *os.File // the open file
	size int64    // bytes in the file so far, also where the next record will start
}

// openSegment opens the segment file at path, creating it if it doesn't exist.
func openSegment(path string) (*segment, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644) // 0o644: owner can read/write, others can only read
	if err != nil {
		return nil, err
	}

	// Ask OS for file's details, since a reopened file may already hold records.
	info, err := file.Stat()
	if err != nil {
		file.Close() // don't leave file open if we can't use it
		return nil, err
	}

	return &segment{file: file, size: info.Size()}, nil // size starts at file's current size (0 if new)

}

// Returns byte position where the record starts. Failed write is cut back off file.
func (s *segment) append(payload []byte) (int64, error) {
	rec, err := record.Encode(payload) // payload -> [length][CRC][payload] bytes
	if err != nil {
		return 0, err
	}

	pos := s.size // record starts where file currently ends
	n, err := s.file.Write(rec)
	if err != nil {
		// Write may have stopped partway, so cut off the partial record.
		// Join also reports the Truncate error, if there is one.
		return 0, errors.Join(err, s.file.Truncate(pos))
	}

	s.size += int64(n) // next record starts after this one
	return pos, nil
}
