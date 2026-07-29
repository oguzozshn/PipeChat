// Package config manages PipeChat's on-disk identity: the ed25519 keypair,
// the authorized_keys allowlist (server side) and the known_hosts trust
// store (client side, trust-on-first-use).
package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Dir returns ~/.pipechat, creating it if necessary.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".pipechat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// LoadOrCreateKey reads an ed25519 private key from path, generating and
// persisting a new one if the file does not yet exist.
func LoadOrCreateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("generating key: %w", genErr)
		}
		block, marshalErr := ssh.MarshalPrivateKey(priv, "")
		if marshalErr != nil {
			return nil, fmt.Errorf("marshaling private key: %w", marshalErr)
		}
		pemBytes := pem.EncodeToMemory(block)
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			return nil, fmt.Errorf("writing key file: %w", err)
		}
		signer, err := ssh.NewSignerFromKey(priv)
		if err != nil {
			return nil, err
		}
		_ = pub
		return signer, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading key file: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing key file %s: %w", path, err)
	}
	return signer, nil
}

// AuthorizedKeysCallback builds a PublicKeyCallback that accepts only keys
// listed in the authorized_keys file at path (created empty if missing).
func AuthorizedKeysCallback(path string) (func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error), error) {
	allowed, err := loadAuthorizedKeys(path)
	if err != nil {
		return nil, err
	}
	return func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		marshaled := string(key.Marshal())
		if _, ok := allowed[marshaled]; ok {
			return nil, nil
		}
		return nil, fmt.Errorf("public key not authorized")
	}, nil
}

func loadAuthorizedKeys(path string) (map[string]bool, error) {
	allowed := map[string]bool{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if writeErr := os.WriteFile(path, nil, 0o600); writeErr != nil {
			return nil, writeErr
		}
		return allowed, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		allowed[string(key.Marshal())] = true
	}
	return allowed, nil
}

// AddAuthorizedKey appends pubkeyLine (a single "ssh-ed25519 AAAA..." line)
// to the authorized_keys file at path, skipping it if already present.
func AddAuthorizedKey(path, pubkeyLine string) error {
	pubkeyLine = strings.TrimSpace(pubkeyLine)
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkeyLine))
	if err != nil {
		return fmt.Errorf("parsing public key: %w", err)
	}
	allowed, err := loadAuthorizedKeys(path)
	if err != nil {
		return err
	}
	if allowed[string(key.Marshal())] {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(pubkeyLine + "\n")
	return err
}

// TOFUHostKeyCallback returns an ssh.HostKeyCallback that trusts a host's key
// on first connection and persists it to the known_hosts file at path;
// subsequent connections require an exact match, rejecting on mismatch.
func TOFUHostKeyCallback(path string) (ssh.HostKeyCallback, error) {
	if _, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE, 0o600); err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		cb, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			f, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if openErr != nil {
				return openErr
			}
			defer f.Close()
			line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
			_, writeErr := f.WriteString(line + "\n")
			return writeErr
		}
		return fmt.Errorf("host key verification failed for %s: %w", hostname, err)
	}, nil
}
