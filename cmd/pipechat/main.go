// Command pipechat is a symmetric, SSH-authenticated point-to-point tool
// for exchanging text messages between two machines. Both "listen" and
// "connect" sides speak the same frame protocol once connected.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"pipechat/internal/config"
	"pipechat/internal/transport"
	"pipechat/internal/ui"
)

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
	fatalIf(ui.Run(channel))
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
	fatalIf(ui.Run(channel))
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

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
