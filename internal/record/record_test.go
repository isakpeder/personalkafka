package record

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestEncodeLayout(t *testing.T) {
	payload := []byte("hello")

	got, err := Encode(payload)
	if err != nil {
		t.Fatalf("Encode: unexpected error: %v", err)
	}

	if len(got) != headerSize+len(payload) {
		t.Fatalf("len = %d, want %d", len(got), headerSize+len(payload))
	}
	if n := binary.BigEndian.Uint32(got[0:4]); n != uint32(len(payload)) {
		t.Errorf("length field = %d, want %d", n, len(payload))
	}
	if !bytes.Equal(got[headerSize:], payload) {
		t.Errorf("payload = %q, want %q", got[headerSize:], payload)
	}
}

func TestEncodeCRCCheckValue(t *testing.T) {
	got, err := Encode([]byte("123456789"))
	if err != nil {
		t.Fatalf("Encode: unexpected error: %v", err)
	}

	// Published CRC-32C check value for "123456789". IEEE would give 0xCBF43926.
	const want = 0xE3069283
	if sum := binary.BigEndian.Uint32(got[4:8]); sum != want {
		t.Errorf("crc = %#x, want %#x", sum, want)
	}
}

func TestEncodeEmpty(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty", []byte{}},
		{"nil", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.payload)
			if err != nil {
				t.Fatalf("Encode: unexpected error: %v", err)
			}
			if len(got) != headerSize {
				t.Fatalf("len = %d, want %d", len(got), headerSize)
			}
			if n := binary.BigEndian.Uint32(got[0:4]); n != 0 {
				t.Errorf("length field = %d, want 0", n)
			}
		})
	}
}

func TestEncodeSizeLimit(t *testing.T) {
	if _, err := Encode(make([]byte, maxPayloadSize)); err != nil {
		t.Errorf("payload at limit: unexpected error: %v", err)
	}

	got, err := Encode(make([]byte, maxPayloadSize+1))
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("payload over limit: err = %v, want ErrTooLarge", err)
	}
	if got != nil {
		t.Errorf("payload over limit: got %d bytes, want nil", len(got))
	}
}

func TestEncodeDoesNotMutatePayload(t *testing.T) {
	payload := []byte("hello")
	saved := bytes.Clone(payload)

	if _, err := Encode(payload); err != nil {
		t.Fatalf("Encode: unexpected error: %v", err)
	}
	if !bytes.Equal(payload, saved) {
		t.Errorf("payload changed: got %q, want %q", payload, saved)
	}
}
