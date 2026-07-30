package ui

import (
	"io"
	"sync"

	"pipechat/internal/protocol"
)

// chanWriter serializes frame writes to the underlying connection. The
// input handler (sending text/file frames) and the read loop (sending an
// Ack after a file arrives) can both write concurrently; without a shared
// lock their frames could interleave and corrupt the stream.
type chanWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *chanWriter) WriteFrame(t protocol.FrameType, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return protocol.WriteFrame(w.w, t, payload)
}
