// Package ui implements PipeChat's Bubble Tea terminal interface: a
// scrollable chat log, an input line, and a progress bar shown during file
// transfers. It sits directly on top of the io.ReadWriteCloser returned by
// internal/transport, so it works identically for the listening and
// connecting side of a session.
package ui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"pipechat/internal/protocol"
)

// Run drives the chat/file-transfer TUI over conn until the connection
// closes or the user quits.
func Run(conn io.ReadWriteCloser) error {
	defer conn.Close()

	events := make(chan tea.Msg, 16)
	cw := &chanWriter{w: conn}
	m := newModel(cw, events)

	p := tea.NewProgram(m)

	go readLoop(conn, cw, events)

	_, err := p.Run()
	return err
}

// readLoop decodes frames off conn and turns them into UI events until the
// connection closes. It owns the receiving side's file-transfer state so a
// slow disk write never blocks the UI, and it is the only goroutine besides
// the model's own writes that touches cw, keeping frame writes serialized.
func readLoop(conn io.Reader, cw *chanWriter, events chan<- tea.Msg) {
	var recv fileReceiver
	for {
		typ, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			events <- connClosedMsg{err: errOrNil(err)}
			return
		}

		switch typ {
		case protocol.TypeText:
			events <- peerTextMsg(payload)

		case protocol.TypeFileHeader:
			hdr, decErr := protocol.DecodeFileHeader(payload)
			if decErr != nil {
				events <- peerTextMsg("[hata] bozuk dosya başlığı: " + decErr.Error())
				continue
			}
			if startErr := recv.start(hdr); startErr != nil {
				events <- peerTextMsg("[hata] dosya oluşturulamadı: " + startErr.Error())
				continue
			}
			events <- fileRecvStartMsg{name: hdr.Name, size: hdr.Size}

		case protocol.TypeFileChunk:
			if writeErr := recv.write(payload); writeErr != nil {
				events <- peerTextMsg("[hata] dosya yazılamadı: " + writeErr.Error())
				continue
			}
			events <- fileRecvProgressMsg{written: recv.written, total: recv.expected}

		case protocol.TypeFileComplete:
			dest, written, expected, ok := recv.finish()
			events <- fileRecvDoneMsg{name: dest, written: written, total: expected, ok: ok}
			ackByte := byte(0)
			if ok {
				ackByte = 1
			}
			_ = cw.WriteFrame(protocol.TypeAck, []byte{ackByte})

		case protocol.TypeAck:
			events <- ackMsg{ok: len(payload) == 1 && payload[0] == 1}
		}
	}
}

// errOrNil turns io.EOF (an ordinary closed-connection signal) into nil so
// the UI reports a plain "connection closed" instead of an EOF error.
func errOrNil(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}
