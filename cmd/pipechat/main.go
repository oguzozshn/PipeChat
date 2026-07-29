// Command pipechat is a symmetric, SSH-authenticated point-to-point tool
// for exchanging text messages between two machines. Both "listen" and
// "connect" sides speak the same frame protocol once connected.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"pipechat/internal/config"
	"pipechat/internal/protocol"
	"pipechat/internal/transport"
)

// fileChunkSize bounds how much of a file is read and sent per
// TypeFileChunk frame, well under protocol.MaxFrameSize. Keeping it small
// limits how long a chunk write can delay an interleaved text message.
const fileChunkSize = 256 * 1024

// chanWriter serializes frame writes to an ssh.Channel. runChat's stdin
// loop and its read goroutine (which sends TypeAck after receiving a file)
// can both write to the channel concurrently; without a shared lock their
// frames could interleave and corrupt the stream.
type chanWriter struct {
	mu sync.Mutex
	ch ssh.Channel
}

func (w *chanWriter) WriteFrame(t protocol.FrameType, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return protocol.WriteFrame(w.ch, t, payload)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "listen":
		cmdListen(os.Args[2:])
	case "connect":
		cmdConnect(os.Args[2:])
	case "authorize":
		cmdAuthorize(os.Args[2:])
	case "pubkey":
		cmdPubkey(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `pipechat <command> [flags]

Commands:
  listen --addr :2222       Wait for a peer to connect and start chatting.
  connect --addr host:2222  Connect to a listening peer and start chatting.
  pubkey                    Print this machine's public key (share it with the peer).
  authorize <pubkey-line>   Allow a peer's public key to connect to this machine.`)
}

func cmdListen(args []string) {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	addr := fs.String("addr", ":2222", "address to listen on")
	fs.Parse(args)

	dir, err := config.Dir()
	fatalIf(err)
	hostKey, err := config.LoadOrCreateKey(filepath.Join(dir, "host_key"))
	fatalIf(err)
	pkCallback, err := config.AuthorizedKeysCallback(filepath.Join(dir, "authorized_keys"))
	fatalIf(err)

	fmt.Fprintf(os.Stderr, "listening on %s ...\n", *addr)
	channel, err := transport.Listen(transport.ServerConfig{
		Addr:              *addr,
		HostKey:           hostKey,
		PublicKeyCallback: pkCallback,
	})
	fatalIf(err)
	fmt.Fprintln(os.Stderr, "peer connected.")
	runChat(channel)
}

func cmdConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	addr := fs.String("addr", "", "address to connect to, e.g. host:2222")
	fs.Parse(args)
	if *addr == "" {
		fmt.Fprintln(os.Stderr, "connect: --addr is required")
		os.Exit(1)
	}

	dir, err := config.Dir()
	fatalIf(err)
	identity, err := config.LoadOrCreateKey(filepath.Join(dir, "id_ed25519"))
	fatalIf(err)
	hostKeyCb, err := config.TOFUHostKeyCallback(filepath.Join(dir, "known_hosts"))
	fatalIf(err)

	fmt.Fprintf(os.Stderr, "connecting to %s ...\n", *addr)
	channel, err := transport.Connect(transport.ClientConfig{
		Addr:            *addr,
		Identity:        identity,
		HostKeyCallback: hostKeyCb,
	})
	fatalIf(err)
	fmt.Fprintln(os.Stderr, "connected.")
	runChat(channel)
}

func cmdPubkey(args []string) {
	dir, err := config.Dir()
	fatalIf(err)
	// This is the identity connect() uses, so it's what the peer's
	// "authorize" command needs to grant.
	signer, err := config.LoadOrCreateKey(filepath.Join(dir, "id_ed25519"))
	fatalIf(err)
	fmt.Println(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
}

func cmdAuthorize(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "authorize: expected a public key line, e.g. \"ssh-ed25519 AAAA...\"")
		os.Exit(1)
	}
	dir, err := config.Dir()
	fatalIf(err)
	fatalIf(config.AddAuthorizedKey(filepath.Join(dir, "authorized_keys"), args[0]))
	fmt.Fprintln(os.Stderr, "authorized.")
}

// runChat pumps incoming frames to stdout and stdin lines to outgoing text
// frames, until either side closes the channel. Lines starting with
// "/send " trigger a file transfer instead of a text message.
func runChat(channel ssh.Channel) {
	defer channel.Close()
	cw := &chanWriter{ch: channel}

	done := make(chan struct{})
	go func() {
		defer close(done)

		var (
			recvFile     *os.File
			recvExpected uint64
			recvWritten  uint64
			recvName     string
		)

		for {
			typ, payload, err := protocol.ReadFrame(channel)
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(os.Stderr, "\nconnection closed: %v\n", err)
				} else {
					fmt.Fprintln(os.Stderr, "\nconnection closed.")
				}
				return
			}

			switch typ {
			case protocol.TypeText:
				fmt.Printf("\rpeer> %s\nyou> ", string(payload))

			case protocol.TypeFileHeader:
				hdr, err := protocol.DecodeFileHeader(payload)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\nbad file header: %v\nyou> ", err)
					continue
				}
				dest, err := safeDestPath(hdr.Name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\nrefusing file %q: %v\nyou> ", hdr.Name, err)
					continue
				}
				f, err := os.Create(dest)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\ncannot create %s: %v\nyou> ", dest, err)
					continue
				}
				recvFile, recvExpected, recvWritten, recvName = f, hdr.Size, 0, dest
				fmt.Printf("\rreceiving %s (%d bytes)...\nyou> ", dest, hdr.Size)

			case protocol.TypeFileChunk:
				if recvFile == nil {
					continue
				}
				n, err := recvFile.Write(payload)
				recvWritten += uint64(n)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\nwrite error for %s: %v\nyou> ", recvName, err)
					recvFile.Close()
					recvFile = nil
				}

			case protocol.TypeFileComplete:
				ok := recvFile != nil && recvWritten == recvExpected
				if recvFile != nil {
					recvFile.Close()
				}
				if ok {
					fmt.Printf("\rreceived %s OK (%d bytes)\nyou> ", recvName, recvWritten)
				} else {
					fmt.Printf("\rfile %s incomplete: got %d, want %d\nyou> ", recvName, recvWritten, recvExpected)
				}
				ackByte := byte(0)
				if ok {
					ackByte = 1
				}
				if err := cw.WriteFrame(protocol.TypeAck, []byte{ackByte}); err != nil {
					fmt.Fprintf(os.Stderr, "\nack send failed: %v\nyou> ", err)
				}
				recvFile, recvExpected, recvWritten, recvName = nil, 0, 0, ""

			case protocol.TypeAck:
				if len(payload) == 1 && payload[0] == 1 {
					fmt.Print("\rpeer confirms file received OK\nyou> ")
				} else {
					fmt.Print("\rpeer reports file transfer FAILED\nyou> ")
				}
			}
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("you> ")
	for scanner.Scan() {
		line := scanner.Text()
		if path, ok := strings.CutPrefix(line, "/send "); ok {
			if err := sendFile(cw, strings.TrimSpace(path)); err != nil {
				fmt.Fprintf(os.Stderr, "send %s: %v\n", path, err)
			}
			fmt.Print("you> ")
			continue
		}
		if err := cw.WriteFrame(protocol.TypeText, []byte(line)); err != nil {
			fmt.Fprintf(os.Stderr, "send failed: %v\n", err)
			break
		}
		fmt.Print("you> ")
	}
	<-done
}

// sendFile streams path to the peer as a TypeFileHeader frame followed by
// TypeFileChunk frames and a closing TypeFileComplete frame. It does not
// wait for the peer's TypeAck; that arrives asynchronously and is handled
// by runChat's read goroutine.
func sendFile(cw *chanWriter, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := protocol.EncodeFileHeader(protocol.FileHeader{
		Size: uint64(stat.Size()),
		Name: filepath.Base(path),
	})
	if err != nil {
		return err
	}
	if err := cw.WriteFrame(protocol.TypeFileHeader, header); err != nil {
		return err
	}

	buf := make([]byte, fileChunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if writeErr := cw.WriteFrame(protocol.TypeFileChunk, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return cw.WriteFrame(protocol.TypeFileComplete, nil)
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

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
