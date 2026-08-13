package loadbalancer

import (
	"context"
	"strings"
	"testing"

	"github.com/Shihasz/canopy/internal/transport"
)

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

func sampleUpstream(pct int) UpstreamConfig {
	return UpstreamConfig{
		UpstreamName: "checkout_svc",
		StableAddr:   "10.0.0.5:8080",
		CanaryAddr:   "10.0.0.6:8080",
		CanaryPct:    pct,
	}
}

func TestRenderUpstream_MidSplit(t *testing.T) {
	out, err := RenderUpstream(sampleUpstream(25))
	if err != nil {
		t.Fatalf("RenderUpstream: %v", err)
	}
	for _, want := range []string{
		"upstream checkout_svc {",
		"server 10.0.0.5:8080 weight=75;",
		"server 10.0.0.6:8080 weight=25;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestRenderUpstream_ZeroPercentUsesMinWeight(t *testing.T) {
	out, err := RenderUpstream(sampleUpstream(0))
	if err != nil {
		t.Fatalf("RenderUpstream: %v", err)
	}
	if !strings.Contains(out, "weight=1;\n}") && !strings.Contains(out, "server 10.0.0.6:8080 weight=1;") {
		t.Errorf("expected canary weight=1 at 0%%, got:\n%s", out)
	}
	if !strings.Contains(out, "server 10.0.0.5:8080 weight=100;") {
		t.Errorf("expected stable weight=100 at 0%% canary, got:\n%s", out)
	}
}

func TestRenderUpstream_HundredPercentUsesMinWeight(t *testing.T) {
	out, err := RenderUpstream(sampleUpstream(100))
	if err != nil {
		t.Fatalf("RenderUpstream: %v", err)
	}
	if !strings.Contains(out, "server 10.0.0.5:8080 weight=1;") {
		t.Errorf("expected stable weight=1 at 100%% canary, got:\n%s", out)
	}
	if !strings.Contains(out, "server 10.0.0.6:8080 weight=100;") {
		t.Errorf("expected canary weight=100, got:\n%s", out)
	}
}

func TestRenderUpstream_InvalidPct(t *testing.T) {
	for _, pct := range []int{-1, 101} {
		if _, err := RenderUpstream(sampleUpstream(pct)); err == nil {
			t.Errorf("expected error for CanaryPct=%d, got nil", pct)
		}
	}
}

func TestController_SetTraffic_Success(t *testing.T) {
	fake := &fakeExecutor{}
	c := NewController(fake, "/etc/nginx/conf.d/canopy-upstream.conf")

	err := c.SetTraffic(context.Background(), sampleUpstream(50))
	if err != nil {
		t.Fatalf("SetTraffic: %v", err)
	}

	if len(fake.calls) != 3 {
		t.Fatalf("expected 3 commands (write, test, reload), got %d: %v", len(fake.calls), fake.calls)
	}
	if !strings.Contains(fake.calls[0], "tee /etc/nginx/conf.d/canopy-upstream.conf") {
		t.Errorf("call[0] = %q, want config write", fake.calls[0])
	}
	if fake.calls[1] != "sudo nginx -t" {
		t.Errorf("call[1] = %q, want validation", fake.calls[1])
	}
	if fake.calls[2] != "sudo nginx -s reload" {
		t.Errorf("call[2] = %q, want reload", fake.calls[2])
	}
}

func TestController_SetTraffic_ValidationFails_NoReload(t *testing.T) {
	fake := &fakeExecutor{
		runFunc: func(command string) (transport.Result, error) {
			if strings.Contains(command, "nginx -t") {
				return transport.Result{ExitCode: 1, Stderr: "syntax error on line 3"}, nil
			}
			return transport.Result{ExitCode: 0}, nil
		},
	}
	c := NewController(fake, "/etc/nginx/conf.d/canopy-upstream.conf")

	err := c.SetTraffic(context.Background(), sampleUpstream(50))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("error = %v, want it to mention the validation failure", err)
	}

	for _, call := range fake.calls {
		if strings.Contains(call, "reload") {
			t.Errorf("reload must not run after failed validation, but got call: %q", call)
		}
	}
}
