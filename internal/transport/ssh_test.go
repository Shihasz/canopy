package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func dialTestServer(t *testing.T, addr string, signer ssh.Signer) *SSHExecutor {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	exec, err := NewSSHExecutor(Config{
		Host:            host,
		Port:            port,
		User:            "testuser",
		Signer:          signer,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSSHExecutor: %v", err)
	}
	return exec
}

func TestSSHExecutor_Run_Success(t *testing.T) {
	addr, signer, cleanup := startTestSSHServer(t)
	defer cleanup()

	exec := dialTestServer(t, addr, signer)
	defer exec.Close()

	result, err := exec.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "echo hello") {
		t.Errorf("Stdout = %q, want it to contain %q", result.Stdout, "echo hello")
	}
}

func TestSSHExecutor_Run_NonZeroExit(t *testing.T) {
	addr, signer, cleanup := startTestSSHServer(t)
	defer cleanup()

	exec := dialTestServer(t, addr, signer)
	defer exec.Close()

	result, err := exec.Run(context.Background(), "fail-command")
	if err != nil {
		t.Fatalf("Run returned a Go error for a non-zero exit, want nil: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "simulated failure") {
		t.Errorf("Stderr = %q, want it to contain %q", result.Stderr, "simulated failure")
	}
}

func TestSSHExecutor_Run_ContextCancelled(t *testing.T) {
	addr, signer, cleanup := startTestSSHServer(t)
	defer cleanup()

	exec := dialTestServer(t, addr, signer)
	defer exec.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := exec.Run(ctx, "sleep-command")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}
}

func TestLoadSigner(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "test-key")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	signer, err := LoadSigner(keyPath)
	if err != nil {
		t.Fatalf("LoadSigner: %v", err)
	}

	want, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("NewSignerFromKey: %v", err)
	}
	if !bytes.Equal(signer.PublicKey().Marshal(), want.PublicKey().Marshal()) {
		t.Error("loaded signer public key does not match original")
	}
}

func TestLoadSigner_MissingFile(t *testing.T) {
	_, err := LoadSigner("/nonexistent/path/id_ed25519")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
