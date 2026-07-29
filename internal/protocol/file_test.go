package protocol

import (
	"bytes"
	"math"
	"testing"
)

func TestEncodeDecodeFileHeaderRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		h    FileHeader
	}{
		{"normal", FileHeader{Size: 12345, Name: "report.pdf"}},
		{"unicode name", FileHeader{Size: 1, Name: "raporüçlü.txt"}},
		{"zero size", FileHeader{Size: 0, Name: "empty.txt"}},
		{"large size", FileHeader{Size: math.MaxUint64, Name: "big.bin"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, err := EncodeFileHeader(c.h)
			if err != nil {
				t.Fatalf("EncodeFileHeader: %v", err)
			}
			got, err := DecodeFileHeader(payload)
			if err != nil {
				t.Fatalf("DecodeFileHeader: %v", err)
			}
			if got != c.h {
				t.Errorf("got %+v, want %+v", got, c.h)
			}
		})
	}
}

func TestDecodeFileHeaderRejectsTruncated(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"too short for fixed fields", []byte{0x00, 0x00, 0x00, 0x00}},
		{"name length exceeds payload", func() []byte {
			p, _ := EncodeFileHeader(FileHeader{Size: 1, Name: "abcdef"})
			return p[:len(p)-3]
		}()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeFileHeader(c.payload); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestFileHeaderFrameRoundTrip(t *testing.T) {
	h := FileHeader{Size: 42, Name: "notes.md"}
	payload, err := EncodeFileHeader(h)
	if err != nil {
		t.Fatalf("EncodeFileHeader: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeFileHeader, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	typ, gotPayload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if typ != TypeFileHeader {
		t.Fatalf("type = %v, want %v", typ, TypeFileHeader)
	}

	got, err := DecodeFileHeader(gotPayload)
	if err != nil {
		t.Fatalf("DecodeFileHeader: %v", err)
	}
	if got != h {
		t.Errorf("got %+v, want %+v", got, h)
	}
}
