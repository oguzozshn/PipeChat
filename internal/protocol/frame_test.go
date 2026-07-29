package protocol

import (
	"bytes"
	"testing"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		typ     FrameType
		payload []byte
	}{
		{"text", TypeText, []byte("merhaba dunya")},
		{"empty", TypeAck, nil},
		{"binary", TypeFileChunk, []byte{0x00, 0xFF, 0x10, 0x20}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, c.typ, c.payload); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			gotType, gotPayload, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if gotType != c.typ {
				t.Errorf("type = %v, want %v", gotType, c.typ)
			}
			if !bytes.Equal(gotPayload, c.payload) {
				t.Errorf("payload = %v, want %v", gotPayload, c.payload)
			}
		})
	}
}

func TestReadFrameRejectsOversized(t *testing.T) {
	var buf bytes.Buffer
	header := []byte{byte(TypeFileChunk), 0xFF, 0xFF, 0xFF, 0xFF}
	buf.Write(header)
	if _, _, err := ReadFrame(&buf); err != ErrFrameTooLarge {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}
