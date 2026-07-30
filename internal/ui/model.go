package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"pipechat/internal/protocol"
)

// Messages produced by the read loop (internal/ui/run.go) and by the
// goroutine started for an outgoing /send, delivered to Update over the
// shared events channel.
type (
	peerTextMsg string

	fileRecvStartMsg struct {
		name string
		size uint64
	}
	fileRecvProgressMsg struct{ written, total uint64 }
	fileRecvDoneMsg     struct {
		name           string
		written, total uint64
		ok             bool
	}

	ackMsg struct{ ok bool }

	fileSendProgressMsg struct {
		name           string
		written, total uint64
	}
	fileSendDoneMsg struct {
		name string
		err  error
	}

	connClosedMsg struct{ err error }
)

const chromeHeight = 3 // input line + progress line + blank separator

type model struct {
	cw       *chanWriter
	events   chan tea.Msg
	input    textinput.Model
	viewport viewport.Model
	progress progress.Model
	lines    []string

	transferActive bool
	transferLabel  string
	transferFrac   float64

	disconnected bool
	quitting     bool
}

func newModel(cw *chanWriter, events chan tea.Msg) model {
	ti := textinput.New()
	ti.Placeholder = "mesaj yaz veya /send <dosya-yolu>"
	ti.Focus()
	ti.CharLimit = 4096

	vp := viewport.New(80, 20)

	return model{
		cw:       cw,
		events:   events,
		input:    ti,
		viewport: vp,
		progress: progress.New(progress.WithDefaultGradient()),
	}
}

func waitForMsg(events chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-events
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitForMsg(m.events))
}

func (m *model) appendLine(s string) {
	m.lines = append(m.lines, s)
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	m.viewport.GotoBottom()
}

func (m model) startSend(path string) {
	displayName := filepath.Base(path)
	name, err := sendFile(m.cw, path, func(written, total uint64) {
		m.events <- fileSendProgressMsg{name: displayName, written: written, total: total}
	})
	m.events <- fileSendDoneMsg{name: name, err: err}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - chromeHeight
		m.progress.Width = msg.Width - 4
		m.viewport.SetContent(strings.Join(m.lines, "\n"))
		m.viewport.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		if m.disconnected {
			m.quitting = true
			return m, tea.Quit
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			line := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			if line == "" {
				return m, nil
			}
			if path, ok := strings.CutPrefix(line, "/send "); ok {
				path = strings.TrimSpace(path)
				m.appendLine(fmt.Sprintf("[gönderiliyor] %s", path))
				go m.startSend(path)
				return m, nil
			}
			if err := m.cw.WriteFrame(protocol.TypeText, []byte(line)); err != nil {
				m.appendLine(fmt.Sprintf("[hata] mesaj gönderilemedi: %v", err))
			} else {
				m.appendLine("sen> " + line)
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

	case peerTextMsg:
		m.appendLine("peer> " + string(msg))
		return m, waitForMsg(m.events)

	case fileRecvStartMsg:
		m.transferActive = true
		m.transferLabel = fmt.Sprintf("alınıyor: %s", msg.name)
		m.transferFrac = 0
		m.appendLine(fmt.Sprintf("[dosya] %s alınıyor (%d bayt)", msg.name, msg.size))
		return m, waitForMsg(m.events)

	case fileRecvProgressMsg:
		if msg.total > 0 {
			m.transferFrac = float64(msg.written) / float64(msg.total)
		}
		return m, waitForMsg(m.events)

	case fileRecvDoneMsg:
		m.transferActive = false
		if msg.ok {
			m.appendLine(fmt.Sprintf("[dosya] alındı: %s (%d bayt)", msg.name, msg.written))
		} else {
			m.appendLine(fmt.Sprintf("[dosya] eksik/hatalı: %s (%d/%d bayt)", msg.name, msg.written, msg.total))
		}
		return m, waitForMsg(m.events)

	case ackMsg:
		if msg.ok {
			m.appendLine("[dosya] karşı taraf teslim aldığını onayladı")
		} else {
			m.appendLine("[dosya] karşı taraf transferin başarısız olduğunu bildirdi")
		}
		return m, waitForMsg(m.events)

	case fileSendProgressMsg:
		m.transferActive = true
		m.transferLabel = fmt.Sprintf("gönderiliyor: %s", msg.name)
		if msg.total > 0 {
			m.transferFrac = float64(msg.written) / float64(msg.total)
		}
		return m, waitForMsg(m.events)

	case fileSendDoneMsg:
		m.transferActive = false
		if msg.err != nil {
			m.appendLine(fmt.Sprintf("[hata] %s gönderilemedi: %v", msg.name, msg.err))
		} else {
			m.appendLine(fmt.Sprintf("[dosya] %s gönderildi, onay bekleniyor...", msg.name))
		}
		return m, waitForMsg(m.events)

	case connClosedMsg:
		m.disconnected = true
		m.input.Blur()
		if msg.err != nil {
			m.appendLine(fmt.Sprintf("[bağlantı kapandı] %v", msg.err))
		} else {
			m.appendLine("[bağlantı kapandı]")
		}
		m.appendLine("(çıkmak için bir tuşa basın)")
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.transferActive {
		b.WriteString(m.transferLabel)
		b.WriteString(" ")
		b.WriteString(m.progress.ViewAs(m.transferFrac))
		b.WriteString("\n")
	}
	b.WriteString(m.input.View())
	return b.String()
}
