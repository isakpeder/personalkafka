package storage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/isakpeder/personalkafka/internal/record"
)

// segment is one file on disk holding records back to back.
type segment struct {
	file       *os.File // the open file
	size       int64    // bytes in the file so far, also where the next record will start
	baseOffset int64    // number of the first record in file
	nextOffset int64    // number next appended record will get
}

// return s the file name for a segment, zero-paddded so names sort in number order
func segmentName(baseOffset int64) string {
	name := fmt.Sprintf("%020d.log", baseOffset) // 1000 -> "00000000000000001000.log"
	return name
}

// parseSegmentName does the reverse of segmentName: file name -> base offset.
func parseSegmentName(name string) (int64, error) {
	digits, hasLog := strings.CutSuffix(name, ".log") // text without ".log" and ifn ".log" was there
	if !hasLog {
		return 0, fmt.Errorf("storage: bad segemnt name %q", name)
	}

	baseOffset, err := strconv.ParseInt(digits, 10, 64) // "00000000000000001000" -> 1000
	if err != nil {                                     // not a number
		return 0, fmt.Errorf("storage: bad segment name %q", name)
	}

	// Reject names segmentName would never make, like "5.log" (not padded) or negative numbers.
	if baseOffset < 0 || segmentName(baseOffset) != name {
		return 0, fmt.Errorf("storage: bad segmnet name %q", name)
	}
	return baseOffset, nil
}

// Returns how many records are in file.
func countRecords(file io.Reader) (int64, error) {
	reader := bufio.NewReader(file) // reads file in big chunks -> so fewer trips to  OS
	var count int64                 // starts at 0

	for {
		_, err := record.Read(reader) // read next record, dont need payload
		if errors.Is(err, io.EOF) {   // no more records
			return count, nil
		}
		if err != nil { // cut off/damaged/disk failure
			return 0, err
		}
		count++
	}
}

// Opens the segment in folder dir that starts at baseOffset, creating it if it doesn't exist
func openSegment(dir string, baseOffset int64) (*segment, error) {
	path := filepath.Join(dir, segmentName(baseOffset))                      // "data" + 1000 -> "data/00000000000000001000.log"
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644) // 0o644: owner can read/write, others can only read
	if err != nil {
		return nil, err
	}

	// Ask OS for file's details cuz a reopened file might already hold records
	info, err := file.Stat()
	if err != nil {
		file.Close() // close if can't use
		return nil, err
	}

	count, err := countRecords(file) // how many records are already in file (0 if new)
	if err != nil {
		file.Close()
		return nil, err
	}

	return &segment{
		file:       file,
		size:       info.Size(),
		baseOffset: baseOffset,
		nextOffset: baseOffset + count,
	}, nil
}

// adds payload as a record at the end of the segmnt, and returns its offset (record number)
// and the byte position where it starts. Failed write is cut back off file.
func (s *segment) append(payload []byte) (offset, pos int64, err error) {
	rec, err := record.Encode(payload) // payload -> [length][CRC][payload] bytes
	if err != nil {
		return 0, 0, err
	}

	offset = s.nextOffset // this record's number
	pos = s.size          // record starts where file currently ends
	n, err := s.file.Write(rec)
	if err != nil {
		// Write mgiht stop partway so cut off the partial record.
		// Join also reports the Truncate error if there is one.
		return 0, 0, errors.Join(err, s.file.Truncate(pos))
	}

	s.size += int64(n) // next record starts after this one
	s.nextOffset++     // and gets the next number
	return offset, pos, nil
}

// returns the payload of the record that starts at byte pos
func (s *segment) readAt(pos int64) ([]byte, error) {
	if pos < 0 || pos > s.size { // pos == s.size is allowed, there's just no record there yet
		return nil, fmt.Errorf("storage: position %d outside segment", pos)
	}

	section := io.NewSectionReader(s.file, pos, s.size-pos) // only the bytes from pos to end of good data
	return record.Read(section)                             // reads one record and checks its CRC
}

// forces the file's data onto physical disk -> ultimately to survive power cut
func (s *segment) sync() error {
	return s.file.Sync()
}

// puts data on the disk, then closes file (closes even if sync fails)
func (s *segment) close() error {
	return errors.Join(s.sync(), s.file.Close())
}

// closes file, then deletes it
func (s *segment) remove() error {
	return errors.Join(s.file.Close(), os.Remove(s.file.Name()))
}
