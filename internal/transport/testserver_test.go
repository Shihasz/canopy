package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startTestSSHServer spins up a minimal in-process SSH server for tests.
// It accepts exactly one client public key and understands three fake
// commands: any normal string echoes it to stdout with exit 0; the literal
// string "fail-command" writes to stderr and exits 1; "sleep-command" sleeps
// 2s before replying, used to test context cancellation.
func startTestSSHServer(t *testing.T) (addr string, clientSigner ssh.Signer, cleanup func()) {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientSigner, err = ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatalf("client signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(clientSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized public key")
		},
	}
	serverConfig.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		for {
			nConn, err := listener.Accept()
			if err != nil {
				return // listener closed during cleanup
			}
			go handleTestConn(nConn, serverConfig)
		}
	}()

	cleanup = func() { _ = listener.Close() }
	return listener.Addr().String(), clientSigner, cleanup
}

func handleTestConn(nConn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go handleTestSession(channel, requests)
	}
}

func handleTestSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for req := range requests {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}

		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}

		exitCode := 0
		switch payload.Command {
		case "fail-command":
			fmt.Fprint(channel.Stderr(), "simulated failure\n")
			exitCode = 1
		case "sleep-command":
			time.Sleep(2 * time.Second)
			fmt.Fprint(channel, "slept\n")
		default:
			fmt.Fprintf(channel, "echo: %s\n", payload.Command)
		}

		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ ExitStatus uint32 }{uint32(exitCode)}))
		return
	}
}
