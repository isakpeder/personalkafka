package record

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const headerSize = 8
const maxPayloadSize = 1048576

var ErrTooLarge = errors.New("record: payload too large") // Sentinel error for data payload exceeds max record size
var ErrIncomplete = errors.New("record: incomplete")      // Sentinel error for incomplete record (short header or payload)
var ErrCorrupt = errors.New("record: corrupt")            // Wrong bytes (CRC mismatch or impossible length)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// Returns a payload framed as a record: its length, its CRC-32C checksum, and a copy of its bytes.
// The result is a signle slice (so it cna be appended to a segment in one write).
func Encode(payload []byte) ([]byte, error) {
	if len(payload) > maxPayloadSize {
		return nil, ErrTooLarge
	}

	buf := make([]byte, len(payload)+headerSize) // Allocate record of bytes

	// Store payload length as 4-byte number so reader knows how many payload bytes follow header.
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(payload)))

	sum := crc32.Checksum(payload, crcTable)  // 'fingerprint' of bytes
	binary.BigEndian.PutUint32(buf[4:8], sum) // store checksum
	copy(buf[headerSize:], payload)           // copies payload bytes into buf

	return buf, nil
}

// Returns the data payload, the number of bytes the record used, and an error
// The returned payload points into buf, not a copy. It changes if buf is changed.
func Decode(buf []byte) ([]byte, int, error) {
	if len(buf) < headerSize {
		return nil, 0, ErrIncomplete
	}

	length := int(binary.BigEndian.Uint32(buf[0:4]))
	if length > maxPayloadSize {
		return nil, 0, ErrCorrupt
	}

	end := headerSize + length
	if len(buf) < end {
		return nil, 0, ErrIncomplete
	}

	payload := buf[headerSize:end]
	want := binary.BigEndian.Uint32(buf[4:8])
	got := crc32.Checksum(payload, crcTable)
	if got != want {
		return nil, 0, ErrCorrupt
	}

	return payload, end, nil
}
