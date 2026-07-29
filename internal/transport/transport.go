// Package transport establishes the symmetric SSH connection between two
// PipeChat instances and exposes the single "pipechat-data" channel that
// carries the protocol frames.
package transport

import (
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"
)

// dataChannelType is the custom SSH channel type PipeChat opens on top of
// the transport connection; both listen and connect sides speak the same
// frame protocol over it, which is what makes the architecture symmetric.
const dataChannelType = "pipechat-data"

// ServerConfig bundles what Listen needs to accept one incoming connection.
type ServerConfig struct {
	Addr               string // e.g. ":2222"
	HostKey            ssh.Signer
	PublicKeyCallback  func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error)
}

// Listen blocks until a single peer connects and opens the data channel,
// then returns that channel as an io.ReadWriteCloser. The listener is
// closed once a connection is accepted (PipeChat is point-to-point).
func Listen(cfg ServerConfig) (ssh.Channel, error) {
	sshCfg := &ssh.ServerConfig{
		PublicKeyCallback: cfg.PublicKeyCallback,
	}
	sshCfg.AddHostKey(cfg.HostKey)

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Addr, err)
	}
	defer listener.Close()

	conn, err := listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accepting connection: %w", err)
	}

	sconn, chans, reqs, err := ssh.NewServerConn(conn, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	go ssh.DiscardRequests(reqs)
	_ = sconn

	for newChannel := range chans {
		if newChannel.ChannelType() != dataChannelType {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return nil, fmt.Errorf("accepting channel: %w", err)
		}
		go ssh.DiscardRequests(requests)
		return channel, nil
	}
	return nil, fmt.Errorf("connection closed before data channel was opened")
}

// ClientConfig bundles what Connect needs to dial the listening peer.
type ClientConfig struct {
	Addr            string // e.g. "host:2222"
	Identity        ssh.Signer
	HostKeyCallback ssh.HostKeyCallback
}

// Connect dials addr, performs the SSH handshake and opens the data
// channel, returning it as an io.ReadWriteCloser.
func Connect(cfg ClientConfig) (ssh.Channel, error) {
	sshCfg := &ssh.ClientConfig{
		User:            "pipechat",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Identity)},
		HostKeyCallback: cfg.HostKeyCallback,
	}

	client, err := ssh.Dial("tcp", cfg.Addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", cfg.Addr, err)
	}

	channel, requests, err := client.OpenChannel(dataChannelType, nil)
	if err != nil {
		return nil, fmt.Errorf("opening data channel: %w", err)
	}
	go ssh.DiscardRequests(requests)
	return channel, nil
}
