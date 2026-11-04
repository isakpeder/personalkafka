package record

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const headerSize = 8
const maxPayloadSize = 1048576

var ErrTooLarge = errors.New("record: payload too large") // Sentinel error for data payload exceeds max record size

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
