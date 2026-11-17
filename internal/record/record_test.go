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

// mustEncode encodes payload and fails the test if that errors.
func mustEncode(t *testing.T, payload []byte) []byte {
	t.Helper()
	rec, err := Encode(payload)
	if err != nil {
		t.Fatalf("Encode: unexpected error: %v", err)
	}
	return rec
}

func TestDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"hello", []byte("hello")},
		{"empty", []byte{}},
		{"nil", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := mustEncode(t, tc.payload)

			payload, n, err := Decode(rec)
			if err != nil {
				t.Fatalf("Decode: unexpected error: %v", err)
			}
			if !bytes.Equal(payload, tc.payload) {
				t.Errorf("payload = %q, want %q", payload, tc.payload)
			}
			if n != len(rec) {
				t.Errorf("n = %d, want %d", n, len(rec))
			}
		})
	}
}

func TestDecodeTruncated(t *testing.T) {
	rec := mustEncode(t, []byte("hello"))

	// A crash can cut a record off at any byte; every prefix must be incomplete.
	for i := 0; i < len(rec); i++ {
		if _, _, err := Decode(rec[:i]); !errors.Is(err, ErrIncomplete) {
			t.Errorf("Decode(rec[:%d]): err = %v, want ErrIncomplete", i, err)
		}
	}
}

func TestDecodeCorrupt(t *testing.T) {
	tests := []struct {
		name string
		pos  int
	}{
		{"payload byte", headerSize},
		{"crc byte", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := mustEncode(t, []byte("hello"))
			rec[tc.pos] ^= 0xFF

			if _, _, err := Decode(rec); !errors.Is(err, ErrCorrupt) {
				t.Errorf("err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestDecodeLengthTooLarge(t *testing.T) {
	rec := mustEncode(t, []byte("hello"))
	binary.BigEndian.PutUint32(rec[0:4], maxPayloadSize+1)

	// Without the max check this would look incomplete, and a reader would
	// wait forever for bytes that never come.
	if _, _, err := Decode(rec); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestDecodeConsecutive(t *testing.T) {
	first := mustEncode(t, []byte("first"))
	second := mustEncode(t, []byte("second"))
	buf := append(first, second...)

	p1, n1, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode first: unexpected error: %v", err)
	}
	p2, n2, err := Decode(buf[n1:])
	if err != nil {
		t.Fatalf("Decode second: unexpected error: %v", err)
	}

	if string(p1) != "first" || string(p2) != "second" {
		t.Errorf("payloads = %q, %q, want \"first\", \"second\"", p1, p2)
	}
	if n1+n2 != len(buf) {
		t.Errorf("n1+n2 = %d, want %d", n1+n2, len(buf))
	}
}
