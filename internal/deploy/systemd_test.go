package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shihasz/canopy/internal/transport"
)

// fakeExecutor is a test double for transport.Executor. It records every
// command it's asked to run, and lets each test decide how to respond via
// runFunc — no real SSH connection involved.
type fakeExecutor struct {
	calls   []string
	runFunc func(command string) (transport.Result, error)
}

var _ transport.Executor = (*fakeExecutor)(nil)

func (f *fakeExecutor) Run(_ context.Context, command string) (transport.Result, error) {
	f.calls = append(f.calls, command)
	if f.runFunc != nil {
		return f.runFunc(command)
	}
	return transport.Result{ExitCode: 0}, nil
}

func (f *fakeExecutor) Close() error { return nil }

func sampleConfig() UnitConfig {
	return UnitConfig{
		ServiceName:      "checkout-svc",
		Description:      "Checkout service",
		ExecStart:        "/opt/checkout-svc/releases/v2.0.0/app",
		WorkingDirectory: "/opt/checkout-svc/releases/v2.0.0",
		User:             "appuser",
		Restart:          "on-failure",
	}
}

func TestRenderUnit(t *testing.T) {
	unit, err := RenderUnit(sampleConfig())
	if err != nil {
		t.Fatalf("RenderUnit: %v", err)
	}
	for _, want := range []string{
		"Description=Checkout service",
		"User=appuser",
		"ExecStart=/opt/checkout-svc/releases/v2.0.0/app",
		"WorkingDirectory=/opt/checkout-svc/releases/v2.0.0",
		"Restart=on-failure",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("rendered unit missing %q\ngot:\n%s", want, unit)
		}
	}
}

func TestDeployer_InstallUnit_Success(t *testing.T) {
	fake := &fakeExecutor{}
	d := NewDeployer(fake)

	err := d.InstallUnit(context.Background(), "checkout-svc", "unit-file-contents")
	if err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}

	if len(fake.calls) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(fake.calls), fake.calls)
	}
	if !strings.Contains(fake.calls[0], "tee /etc/systemd/system/checkout-svc.service") {
		t.Errorf("first call = %q, want it to write the unit file", fake.calls[0])
	}
	if fake.calls[1] != "sudo systemctl daemon-reload" {
		t.Errorf("second call = %q, want daemon-reload", fake.calls[1])
	}
}

func TestDeployer_InstallUnit_WriteFails(t *testing.T) {
	fake := &fakeExecutor{
		runFunc: func(command string) (transport.Result, error) {
			if strings.Contains(command, "tee") {
				return transport.Result{ExitCode: 1, Stderr: "permission denied"}, nil
			}
			return transport.Result{ExitCode: 0}, nil
		},
	}
	d := NewDeployer(fake)

	err := d.InstallUnit(context.Background(), "checkout-svc", "unit-file-contents")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %v, want it to mention stderr", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("daemon-reload should not run after a failed write; got calls: %v", fake.calls)
	}
}

func TestDeployer_StartStopRestart(t *testing.T) {
	fake := &fakeExecutor{}
	d := NewDeployer(fake)
	ctx := context.Background()

	if err := d.Start(ctx, "checkout-svc"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Stop(ctx, "checkout-svc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := d.Restart(ctx, "checkout-svc"); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	want := []string{
		"sudo systemctl start checkout-svc",
		"sudo systemctl stop checkout-svc",
		"sudo systemctl restart checkout-svc",
	}
	if len(fake.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
	for i, w := range want {
		if fake.calls[i] != w {
			t.Errorf("call[%d] = %q, want %q", i, fake.calls[i], w)
		}
	}
}

func TestDeployer_Status(t *testing.T) {
	tests := []struct {
		stdout string
		want   ServiceStatus
	}{
		{"active\n", StatusActive},
		{"inactive\n", StatusInactive},
		{"failed\n", StatusFailed},
		{"activating\n", StatusUnknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.want), func(t *testing.T) {
			fake := &fakeExecutor{
				runFunc: func(string) (transport.Result, error) {
					return transport.Result{ExitCode: 1, Stdout: tt.stdout}, nil
				},
			}
			d := NewDeployer(fake)

			got, err := d.Status(context.Background(), "checkout-svc")
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got != tt.want {
				t.Errorf("Status = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDeployer_Status_TransportError(t *testing.T) {
	fake := &fakeExecutor{
		runFunc: func(string) (transport.Result, error) {
			return transport.Result{}, errors.New("connection reset")
		},
	}
	d := NewDeployer(fake)

	_, err := d.Status(context.Background(), "checkout-svc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDeployer_Deploy_FullFlow(t *testing.T) {
	fake := &fakeExecutor{}
	d := NewDeployer(fake)

	if err := d.Deploy(context.Background(), sampleConfig()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(fake.calls) != 3 {
		t.Fatalf("expected 3 commands (write, daemon-reload, restart), got %d: %v", len(fake.calls), fake.calls)
	}
	if !strings.Contains(fake.calls[0], "tee") {
		t.Errorf("call[0] = %q, want unit file write", fake.calls[0])
	}
	if fake.calls[1] != "sudo systemctl daemon-reload" {
		t.Errorf("call[1] = %q, want daemon-reload", fake.calls[1])
	}
	if fake.calls[2] != "sudo systemctl restart checkout-svc" {
		t.Errorf("call[2] = %q, want restart", fake.calls[2])
	}
}

func TestDeployer_Deploy_RestartFails(t *testing.T) {
	fake := &fakeExecutor{
		runFunc: func(command string) (transport.Result, error) {
			if strings.Contains(command, "restart") {
				return transport.Result{ExitCode: 1, Stderr: "start request repeated too quickly"}, nil
			}
			return transport.Result{ExitCode: 0}, nil
		},
	}
	d := NewDeployer(fake)

	err := d.Deploy(context.Background(), sampleConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDeployer_Rollback_UsesDeployPath(t *testing.T) {
	fake := &fakeExecutor{}
	d := NewDeployer(fake)

	priorCfg := sampleConfig()
	priorCfg.ExecStart = "/opt/checkout-svc/releases/v1.9.0/app"

	if err := d.Rollback(context.Background(), priorCfg); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("expected 3 commands, got %d: %v", len(fake.calls), fake.calls)
	}
	if !strings.Contains(fake.calls[0], "v1.9.0") {
		t.Errorf("call[0] = %q, want it to reference the prior version's path", fake.calls[0])
	}
}
