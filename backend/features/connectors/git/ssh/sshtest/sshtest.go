// Package sshtest serves bare git repositories over SSH for tests (forge
// 24f): an in-process SSH server (golang.org/x/crypto/ssh) that accepts
// one client key and runs git's own git-upload-pack and git-receive-pack
// on the repository a command names, under its root.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Server is a running SSH git server.
type Server struct {
	// Addr is host:port; URL gives a repository's clone URL.
	Addr string
	// KnownHosts is the server's host key line for the connection's
	// knownHosts, as ssh-keyscan would print it.
	KnownHosts string
	root       string
	ln         net.Listener
	wg         sync.WaitGroup
	mu         sync.Mutex
	commands   []string
}

// URL is the ssh:// clone URL of a repository under the root.
func (s *Server) URL(repo string) string { return "ssh://git@" + s.Addr + "/" + repo }

// Commands are the git commands run so far.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// Key makes an ed25519 key pair: the private key in OpenSSH form,
// encrypted with passphrase unless it is empty, and its public key.
func Key(t testing.TB, passphrase string) (private string, public ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "test key")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test key", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block)), sp
}

// Start serves the bare repositories under root to clients holding
// authorized's private key, until the test ends.
func Start(t testing.TB, root string, authorized ssh.PublicKey) *Server {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(authorized.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("unknown key")
	}}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: ln.Addr().String(), root: root, ln: ln}
	s.KnownHosts = "[127.0.0.1]:" + strings.TrimPrefix(s.Addr, "127.0.0.1:") + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey())))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.serve(c, cfg) }()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); s.wg.Wait() })
	return s
}

func (s *Server) serve(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "sessions only")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.session(ch, creqs)
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for r := range reqs {
		if r.Type != "exec" {
			if r.WantReply {
				_ = r.Reply(r.Type == "env", nil)
			}
			continue
		}
		var p struct{ Command string }
		if err := ssh.Unmarshal(r.Payload, &p); err != nil {
			_ = r.Reply(false, nil)
			return
		}
		prog, arg, _ := strings.Cut(p.Command, " ")
		repo := strings.Trim(strings.TrimSpace(arg), "'\"/")
		if (prog != "git-upload-pack" && prog != "git-receive-pack") || strings.Contains(repo, "..") {
			_ = r.Reply(false, nil)
			return
		}
		_ = r.Reply(true, nil)
		s.mu.Lock()
		s.commands = append(s.commands, prog+" "+repo)
		s.mu.Unlock()
		cmd := exec.Command(prog, filepath.Join(s.root, repo))
		cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
		// stdin copied by hand: Wait would wait for a copy that ends only
		// when the client closes, after it has read our answer
		in, _ := cmd.StdinPipe()
		status := uint32(0)
		err := cmd.Start()
		if err == nil {
			go func() { _, _ = io.Copy(in, ch); _ = in.Close() }()
			err = cmd.Wait()
		}
		if err != nil {
			status = 1
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				status = uint32(ee.ExitCode())
			}
		}
		_ = ch.CloseWrite()
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, status)
		_, _ = ch.SendRequest("exit-status", false, b)
		return
	}
}
