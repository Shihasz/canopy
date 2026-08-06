// Package transport provides remote command execution over SSH.
package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

// Result is the outcome of running a command on a remote host.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Executor runs shell commands on a remote host. Defined as an interface
// so later packages (systemd, nginx) can depend on this instead of a
// concrete SSH type, making them testable with a fake.
type Executor interface {
	Run(ctx context.Context, command string) (Result, error)
	Close() error
}

// Config holds everything needed to establish an SSH connection.
type Config struct {
	Host string
	Port int
	User string

	// Signer authenticates the client. Callers load this from a private
	// key file via LoadSigner, or inject one directly (useful in tests).
	Signer ssh.Signer

	// HostKeyCallback verifies the server's host key. If nil, it defaults
	// to ssh.InsecureIgnoreHostKey(), which accepts any host key.
	//
	// NOTE: InsecureIgnoreHostKey is fine for the local docker-compose lab
	// environment this project tests against, but a real deployment should
	// supply a callback backed by a known_hosts file — see
	// golang.org/x/crypto/ssh/knownhosts.
	HostKeyCallback ssh.HostKeyCallback

	// Timeout bounds the initial TCP+SSH handshake. Defaults to 10s.
	Timeout time.Duration
}

// SSHExecutor implements Executor over a real SSH connection.
type SSHExecutor struct {
	client *ssh.Client
}

var _ Executor = (*SSHExecutor)(nil)

// NewSSHExecutor dials the target host and returns a ready-to-use executor.
func NewSSHExecutor(cfg Config) (*SSHExecutor, error) {
	if cfg.Signer == nil {
		return nil, fmt.Errorf("transport: Config.Signer is required")
	}

	hostKeyCallback := cfg.HostKeyCallback
	if hostKeyCallback == nil {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	clientConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(cfg.Signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("transport: dial %s: %w", addr, err)
	}

	return &SSHExecutor{client: client}, nil
}

// Run executes command on the remote host and captures its output.
// A non-zero exit code is reported via Result.ExitCode, not as a Go error —
// only transport-level failures (dial issues, connection drops) become errors.
// If ctx is cancelled or times out before the command finishes, Run returns
// ctx.Err().
func (e *SSHExecutor) Run(ctx context.Context, command string) (Result, error) {
	session, err := e.client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("transport: new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return Result{}, ctx.Err()
	case runErr := <-done:
		exitCode := 0
		if runErr != nil {
			var exitErr *ssh.ExitError
			if errors.As(runErr, &exitErr) {
				exitCode = exitErr.ExitStatus()
			} else {
				return Result{}, fmt.Errorf("transport: run command: %w", runErr)
			}
		}
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		}, nil
	}
}

// Close terminates the underlying SSH connection.
func (e *SSHExecutor) Close() error {
	return e.client.Close()
}

// LoadSigner reads and parses a private key file (PEM-encoded, OpenSSH or
// PKCS#8 format) for use as an SSH authentication credential.
func LoadSigner(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("transport: read private key %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("transport: parse private key %q: %w", path, err)
	}
	return signer, nil
}
