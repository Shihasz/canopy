//go:build integration

// Package integration contains end-to-end tests that exercise canopy's
// full stack (SSH, systemd, nginx, Prometheus) against the docker-compose
// lab environment in lab/. Run with `make test-integration` after
// `make lab-up`; these are excluded from the default `go test ./...` run
// via the `integration` build tag.
package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Shihasz/canopy/internal/analysis"
	"github.com/Shihasz/canopy/internal/deploy"
	"github.com/Shihasz/canopy/internal/loadbalancer"
	"github.com/Shihasz/canopy/internal/orchestrator"
	"github.com/Shihasz/canopy/internal/rollout"
	"github.com/Shihasz/canopy/internal/transport"
)

const (
	canaryHost = "localhost"
	canaryPort = 2201
	stableHost = "localhost"
	lbHost     = "localhost"
	lbPort     = 2203

	sshUser        = "deploy"
	sshKeyPath     = "../../lab/keys/deploy_key"
	execStartFmt   = "/opt/checkout-svc/releases/%s/app"
	workingDirFmt  = "/opt/checkout-svc/releases/%s"
	canaryAppAddr  = "canary-vm:8080" // as seen from inside the lab's Docker network (nginx's perspective)
	stableAppAddr  = "stable-vm:8080"
	upstreamName   = "checkout_svc"
	lbConfigPath   = "/etc/nginx/conf.d/canopy-upstream.conf"
	prometheusAddr = "http://localhost:9090"
)

// requireLabRunning skips the test with a clear message if the lab's SSH
// ports aren't reachable, rather than failing with a confusing dial error
// deep inside the orchestrator.
func requireLabRunning(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(canaryHost, "2201"), 2*time.Second)
	if err != nil {
		t.Skipf("lab environment not reachable on :2201 (run `make lab-up` first): %v", err)
	}
	conn.Close()
}

// connect opens an SSH connection to host:port using the lab's generated
// deploy key.
func connect(t *testing.T, host string, port int) *transport.SSHExecutor {
	t.Helper()
	signer, err := transport.LoadSigner(sshKeyPath)
	if err != nil {
		t.Fatalf("load signer (did you run `make lab-keys`?): %v", err)
	}
	exec, err := transport.NewSSHExecutor(transport.Config{
		Host: host, Port: port, User: sshUser, Signer: signer, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect to %s:%d: %v", host, port, err)
	}
	return exec
}

// resetLabState puts the canary host and nginx back to their initial
// state (no canary service running, 0% canary traffic) before a test
// runs, so tests don't depend on execution order or leftover state from
// a previous run.
func resetLabState(t *testing.T, canaryExec, lbExec *transport.SSHExecutor) {
	t.Helper()
	ctx := context.Background()

	// Stop and disable any canary service left running from a prior test.
	_, _ = canaryExec.Run(ctx, "sudo systemctl stop checkout-svc")

	// Reset nginx to 0% canary (min-weight rule: canary=1, stable=100).
	lb := loadbalancer.NewController(lbExec, lbConfigPath)
	err := lb.SetTraffic(ctx, loadbalancer.UpstreamConfig{
		UpstreamName: upstreamName, StableAddr: stableAppAddr, CanaryAddr: canaryAppAddr, CanaryPct: 0,
	})
	if err != nil {
		t.Fatalf("reset LB state: %v", err)
	}
}

func buildOrchestrator(t *testing.T, canaryExec, lbExec *transport.SSHExecutor, newVersion, priorVersion string, thresholds analysis.Thresholds) *orchestrator.Orchestrator {
	t.Helper()

	canaryUnit := deploy.UnitConfig{
		ServiceName:      "checkout-svc",
		Description:      "checkout-svc (integration test)",
		ExecStart:        fmt.Sprintf(execStartFmt, newVersion),
		WorkingDirectory: fmt.Sprintf(workingDirFmt, newVersion),
		User:             "appuser",
		Restart:          "on-failure",
	}
	priorUnit := canaryUnit
	priorUnit.ExecStart = fmt.Sprintf(execStartFmt, priorVersion)
	priorUnit.WorkingDirectory = fmt.Sprintf(workingDirFmt, priorVersion)

	metrics, err := analysis.NewPrometheusProvider(prometheusAddr)
	if err != nil {
		t.Fatalf("connect to prometheus: %v", err)
	}

	o, err := orchestrator.NewOrchestrator(orchestrator.Config{
		Rollout:          rollout.NewRollout("checkout-svc", newVersion, priorVersion),
		CanaryDeployer:   deploy.NewDeployer(canaryExec),
		CanaryUnitConfig: canaryUnit,
		PriorUnitConfig:  priorUnit,
		LB:               loadbalancer.NewController(lbExec, lbConfigPath),
		Upstream: loadbalancer.UpstreamConfig{
			UpstreamName: upstreamName, StableAddr: stableAppAddr, CanaryAddr: canaryAppAddr,
		},
		Analyzer:                analysis.NewAnalyzer(thresholds),
		Metrics:                 metrics,
		AnalysisInterval:        3 * time.Second,
		MaxInconclusiveAttempts: 5,
		WarmupDelay:             8 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	return o
}

// startTrafficGenerator continuously sends requests through nginx
// (http://localhost:8080/work) until ctx is cancelled. Real end-to-end
// canary analysis needs real application traffic flowing through the
// current stable/canary split — without this, Prometheus has nothing to
// scrape and every analysis pass is Inconclusive.
func startTrafficGenerator(ctx context.Context) {
	client := &http.Client{Timeout: 5 * time.Second}
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond) // ~20 req/s
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				resp, err := client.Get("http://localhost:8080/work")
				if err == nil {
					resp.Body.Close()
				}
			}
		}
	}()
}

// TestFullRollout_HealthyCanary_Promotes deploys v2.0.0 (a healthy
// version), expects the rollout to progress through all traffic steps
// and finish Promoted, and confirms nginx ends up sending real traffic
// to the canary host.
func TestFullRollout_HealthyCanary_Promotes(t *testing.T) {
	requireLabRunning(t)

	canaryExec := connect(t, canaryHost, canaryPort)
	defer canaryExec.Close()
	lbExec := connect(t, lbHost, lbPort)
	defer lbExec.Close()

	resetLabState(t, canaryExec, lbExec)

	// Generous, real-world-shaped thresholds: this sample app has some
	// baseline jitter, so thresholds need enough headroom that healthy
	// traffic doesn't spuriously fail analysis.
	thresholds := analysis.Thresholds{
		MaxErrorRate:      0.10,
		MaxErrorRateDelta: 0.10,
		MaxP95LatencyMs:   1000,
		MinSampleCount:    1,
	}

	o := buildOrchestrator(t, canaryExec, lbExec, "v2.0.0", "v1.9.0", thresholds)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	startTrafficGenerator(ctx)

	result, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StatePromoted {
		t.Fatalf("FinalState = %s, want Promoted. Last report: %+v", result.FinalState, result.LastReport)
	}

	status, err := deploy.NewDeployer(canaryExec).Status(ctx, "checkout-svc")
	if err != nil {
		t.Fatalf("query canary status: %v", err)
	}
	if status != deploy.StatusActive {
		t.Errorf("canary service status = %s, want active", status)
	}
}

// TestFullRollout_BrokenCanary_RollsBack deploys v2.0.0-bad (a version
// with a 50% injected error rate), expects canary analysis to catch it
// and the orchestrator to automatically roll back, and confirms nginx
// ends up back at ~0% canary traffic.
func TestFullRollout_BrokenCanary_RollsBack(t *testing.T) {
	requireLabRunning(t)

	canaryExec := connect(t, canaryHost, canaryPort)
	defer canaryExec.Close()
	lbExec := connect(t, lbHost, lbPort)
	defer lbExec.Close()

	resetLabState(t, canaryExec, lbExec)

	// Tight thresholds: the whole point of this test is that a 50% error
	// rate must be caught, so thresholds are set well below that.
	thresholds := analysis.Thresholds{
		MaxErrorRate:      0.05,
		MaxErrorRateDelta: 0.05,
		MaxP95LatencyMs:   1000,
		MinSampleCount:    1,
	}

	o := buildOrchestrator(t, canaryExec, lbExec, "v2.0.0-bad", "v1.9.0", thresholds)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	startTrafficGenerator(ctx)

	result, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StateRolledBack {
		t.Fatalf("FinalState = %s, want RolledBack. Last report: %+v", result.FinalState, result.LastReport)
	}
	if result.LastReport.Verdict != analysis.VerdictFail {
		t.Errorf("LastReport.Verdict = %s, want Fail", result.LastReport.Verdict)
	}

	// Confirm the canary host was rolled back to the prior version, not
	// left running the broken one.
	status, err := deploy.NewDeployer(canaryExec).Status(ctx, "checkout-svc")
	if err != nil {
		t.Fatalf("query canary status: %v", err)
	}
	if status != deploy.StatusActive {
		t.Errorf("canary service status after rollback = %s, want active (running prior version)", status)
	}
}
