// Package protocol implements the length-prefixed frame format used to
// multiplex text messages and file transfers over a single SSH channel.
package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

type FrameType byte

const (
	TypeText FrameType = iota + 1
	TypeFileHeader
	TypeFileChunk
	TypeFileComplete
	TypeAck
)

// MaxFrameSize bounds the payload length accepted from the wire, guarding
// against a corrupt or hostile length prefix triggering a huge allocation.
const MaxFrameSize = 32 * 1024 * 1024

var ErrFrameTooLarge = errors.New("protocol: frame payload exceeds MaxFrameSize")

// WriteFrame writes a single frame: [1 byte type][4 byte big-endian length][payload].
func WriteFrame(w io.Writer, t FrameType, payload []byte) error {
	header := make([]byte, 5)
	header[0] = byte(t)
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads a single frame previously written by WriteFrame.
func ReadFrame(r io.Reader) (FrameType, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > MaxFrameSize {
		return 0, nil, ErrFrameTooLarge
	}
	if length == 0 {
		return FrameType(header[0]), nil, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return FrameType(header[0]), payload, nil
}
