package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"pipechat/internal/protocol"
)

// fileChunkSize bounds how much of a file is read and sent per
// TypeFileChunk frame, well under protocol.MaxFrameSize. Keeping it small
// limits how long a chunk write can delay an interleaved text message.
const fileChunkSize = 256 * 1024

// sendFile streams path to the peer as a TypeFileHeader frame followed by
// TypeFileChunk frames and a closing TypeFileComplete frame, reporting
// progress via onProgress after every chunk. It does not wait for the
// peer's TypeAck; that arrives asynchronously over the same connection.
func sendFile(cw *chanWriter, path string, onProgress func(written, total uint64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	total := uint64(stat.Size())

	header, err := protocol.EncodeFileHeader(protocol.FileHeader{Size: total, Name: name})
	if err != nil {
		return name, err
	}
	if err := cw.WriteFrame(protocol.TypeFileHeader, header); err != nil {
		return name, err
	}

	buf := make([]byte, fileChunkSize)
	var written uint64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if writeErr := cw.WriteFrame(protocol.TypeFileChunk, buf[:n]); writeErr != nil {
				return name, writeErr
			}
			written += uint64(n)
			if onProgress != nil {
				onProgress(written, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return name, err
		}
	}

	return name, cw.WriteFrame(protocol.TypeFileComplete, nil)
}

// fileReceiver accumulates an in-progress incoming file transfer across
// TypeFileHeader/TypeFileChunk/TypeFileComplete frames. It is owned by the
// read loop, not the UI model, so a slow disk never blocks rendering.
type fileReceiver struct {
	f        *os.File
	dest     string
	expected uint64
	written  uint64
}

func (r *fileReceiver) start(hdr protocol.FileHeader) error {
	dest, err := safeDestPath(hdr.Name)
	if err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	r.f, r.dest, r.expected, r.written = f, dest, hdr.Size, 0
	return nil
}

func (r *fileReceiver) write(data []byte) error {
	if r.f == nil {
		return fmt.Errorf("no active transfer")
	}
	n, err := r.f.Write(data)
	r.written += uint64(n)
	return err
}

// finish closes the destination file and reports whether the received byte
// count matched the declared size.
func (r *fileReceiver) finish() (dest string, written, expected uint64, ok bool) {
	dest, written, expected = r.dest, r.written, r.expected
	if r.f != nil {
		r.f.Close()
	}
	ok = r.f != nil && written == expected
	r.f, r.dest, r.expected, r.written = nil, "", 0, 0
	return
}

// safeDestPath reduces an untrusted, peer-supplied file name to its base
// element (defeating path traversal) and joins it with the current working
// directory, so incoming files always land next to the running process.
func safeDestPath(name string) (string, error) {
	base := filepath.Base(name)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", fmt.Errorf("invalid file name %q", name)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, base), nil
}
