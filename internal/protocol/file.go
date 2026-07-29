package protocol

import (
	"encoding/binary"
	"errors"
)

// FileHeader is the decoded payload of a TypeFileHeader frame, sent once at
// the start of a file transfer to announce the file's name and total size.
type FileHeader struct {
	Size uint64
	Name string
}

var (
	ErrFileHeaderTruncated   = errors.New("protocol: file header payload truncated")
	ErrFileHeaderNameTooLong = errors.New("protocol: file header name exceeds 65535 bytes")
)

// EncodeFileHeader serializes h as:
// [8 byte big-endian size][2 byte big-endian name length][name bytes].
func EncodeFileHeader(h FileHeader) ([]byte, error) {
	if len(h.Name) > 0xFFFF {
		return nil, ErrFileHeaderNameTooLong
	}
	buf := make([]byte, 8+2+len(h.Name))
	binary.BigEndian.PutUint64(buf[0:8], h.Size)
	binary.BigEndian.PutUint16(buf[8:10], uint16(len(h.Name)))
	copy(buf[10:], h.Name)
	return buf, nil
}

// DecodeFileHeader parses a payload produced by EncodeFileHeader. It returns
// an error rather than panicking on truncated input, since the payload
// comes straight off the wire from an untrusted peer.
func DecodeFileHeader(payload []byte) (FileHeader, error) {
	if len(payload) < 10 {
		return FileHeader{}, ErrFileHeaderTruncated
	}
	size := binary.BigEndian.Uint64(payload[0:8])
	nameLen := binary.BigEndian.Uint16(payload[8:10])
	if len(payload[10:]) < int(nameLen) {
		return FileHeader{}, ErrFileHeaderTruncated
	}
	return FileHeader{Size: size, Name: string(payload[10 : 10+int(nameLen)])}, nil
}

// TypeFileChunk payloads are raw file bytes with no additional framing.
//
// TypeFileComplete payloads are empty: the receiver already knows the
// expected size from the FileHeader and compares it against the bytes
// accumulated from chunks.
//
// TypeAck payloads are a single byte following a TypeFileComplete: 1 means
// the receiver's byte count matched the declared size, 0 means it didn't.
