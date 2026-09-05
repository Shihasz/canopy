package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Shihasz/canopy/internal/analysis"
	"github.com/Shihasz/canopy/internal/deploy"
	"github.com/Shihasz/canopy/internal/loadbalancer"
	"github.com/Shihasz/canopy/internal/rollout"
	"github.com/Shihasz/canopy/internal/transport"
)

// fakeExecutor mirrors the ones in internal/deploy and internal/loadbalancer
// (kept package-local per Go convention: internal/ test files can't be
// shared across packages without a dedicated test-support package, which
// isn't worth it at this project's size).
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

// fakeMetricsProvider returns canned snapshots per "round" (one round =
// one stable+canary pair). If more rounds are requested than snapshots
// provided, the last snapshot is reused — convenient for tests where most
// steps should just stay healthy after an initial interesting case.
type fakeMetricsProvider struct {
	stableSnaps []analysis.MetricSnapshot
	canarySnaps []analysis.MetricSnapshot
	calls       []string
	round       int
}

func (f *fakeMetricsProvider) Snapshot(_ context.Context, target string) (analysis.MetricSnapshot, error) {
	f.calls = append(f.calls, target)

	idx := f.round
	if idx >= len(f.stableSnaps) {
		idx = len(f.stableSnaps) - 1
	}

	if target == "stable" {
		return f.stableSnaps[idx], nil
	}
	snap := f.canarySnaps[idx]
	f.round++ // advance after the canary read, which is the second call each round
	return snap, nil
}

var (
	healthySnapshot      = analysis.MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	unhealthySnapshot    = analysis.MetricSnapshot{ErrorRate: 0.20, P95LatencyMs: 900, SampleCount: 1000}
	inconclusiveSnapshot = analysis.MetricSnapshot{ErrorRate: 0.0, P95LatencyMs: 50, SampleCount: 5}
)

func testThresholds() analysis.Thresholds {
	return analysis.Thresholds{
		MaxErrorRate:      0.05,
		MaxErrorRateDelta: 0.02,
		MaxP95LatencyMs:   500,
		MinSampleCount:    50,
	}
}

func testUnitConfigs() (canary, prior deploy.UnitConfig) {
	canary = deploy.UnitConfig{
		ServiceName: "checkout-svc", User: "appuser", Restart: "on-failure",
		ExecStart: "/opt/checkout-svc/releases/v2.0.0/app", WorkingDirectory: "/opt/checkout-svc/releases/v2.0.0",
	}
	prior = canary
	prior.ExecStart = "/opt/checkout-svc/releases/v1.9.0/app"
	prior.WorkingDirectory = "/opt/checkout-svc/releases/v1.9.0"
	return canary, prior
}

func testUpstream() loadbalancer.UpstreamConfig {
	return loadbalancer.UpstreamConfig{
		UpstreamName: "checkout_svc",
		StableAddr:   "10.0.0.5:8080",
		CanaryAddr:   "10.0.0.6:8080",
	}
}

// newTestOrchestrator builds an Orchestrator with fake transports for
// canary deploy and load balancer, wired to the given metrics provider.
func newTestOrchestrator(t *testing.T, metrics analysis.MetricsProvider, canaryExec, lbExec *fakeExecutor, maxAttempts int, interval time.Duration) (*Orchestrator, *rollout.Rollout) {
	t.Helper()
	canaryCfg, priorCfg := testUnitConfigs()
	r := rollout.NewRollout("checkout-svc", "v2.0.0", "v1.9.0")

	o, err := NewOrchestrator(Config{
		Rollout:                 r,
		CanaryDeployer:          deploy.NewDeployer(canaryExec),
		CanaryUnitConfig:        canaryCfg,
		PriorUnitConfig:         priorCfg,
		LB:                      loadbalancer.NewController(lbExec, "/etc/nginx/conf.d/canopy-upstream.conf"),
		Upstream:                testUpstream(),
		Analyzer:                analysis.NewAnalyzer(testThresholds()),
		Metrics:                 metrics,
		AnalysisInterval:        interval,
		MaxInconclusiveAttempts: maxAttempts,
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	return o, r
}

func TestNewOrchestrator_MissingRollout(t *testing.T) {
	canaryCfg, _ := testUnitConfigs()
	_, err := NewOrchestrator(Config{
		CanaryDeployer:   deploy.NewDeployer(&fakeExecutor{}),
		CanaryUnitConfig: canaryCfg,
		LB:               loadbalancer.NewController(&fakeExecutor{}, "/path"),
		Analyzer:         analysis.NewAnalyzer(testThresholds()),
		Metrics:          &fakeMetricsProvider{},
	})
	if err == nil {
		t.Fatal("expected error for missing Rollout, got nil")
	}
}

func TestNewOrchestrator_DefaultsApplied(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	o, _ := newTestOrchestrator(t, &fakeMetricsProvider{}, canaryExec, lbExec, 0, 0)
	if o.cfg.AnalysisInterval != 5*time.Second {
		t.Errorf("AnalysisInterval default = %v, want 5s", o.cfg.AnalysisInterval)
	}
	if o.cfg.MaxInconclusiveAttempts != 3 {
		t.Errorf("MaxInconclusiveAttempts default = %d, want 3", o.cfg.MaxInconclusiveAttempts)
	}
}

func TestOrchestrator_Run_FullPromotion(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{healthySnapshot},
	}
	o, r := newTestOrchestrator(t, metrics, canaryExec, lbExec, 3, time.Millisecond)

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StatePromoted {
		t.Errorf("FinalState = %s, want Promoted", result.FinalState)
	}
	if r.TrafficPct != 100 {
		t.Errorf("TrafficPct = %d, want 100", r.TrafficPct)
	}

	// All four traffic steps should have been written to the LB.
	for _, wantWeight := range []string{"weight=10", "weight=25", "weight=50", "weight=100"} {
		found := false
		for _, call := range lbExec.calls {
			if strings.Contains(call, wantWeight) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected an LB write containing %q, calls: %v", wantWeight, lbExec.calls)
		}
	}
}

func TestOrchestrator_Run_FailsAndRollsBack(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{unhealthySnapshot},
	}
	o, r := newTestOrchestrator(t, metrics, canaryExec, lbExec, 3, time.Millisecond)

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StateRolledBack {
		t.Errorf("FinalState = %s, want RolledBack", result.FinalState)
	}
	if r.TrafficPct != 0 {
		t.Errorf("TrafficPct = %d, want 0 after rollback", r.TrafficPct)
	}

	foundPriorVersion := false
	for _, call := range canaryExec.calls {
		if strings.Contains(call, "v1.9.0") {
			foundPriorVersion = true
		}
	}
	if !foundPriorVersion {
		t.Errorf("expected canary deployer to redeploy the prior version, calls: %v", canaryExec.calls)
	}

	foundZeroTraffic := false
	for _, call := range lbExec.calls {
		if strings.Contains(call, "weight=1;") && strings.Contains(call, "weight=100;") {
			foundZeroTraffic = true
		}
	}
	if !foundZeroTraffic {
		t.Errorf("expected LB to be set back to ~0%% canary traffic, calls: %v", lbExec.calls)
	}
}

func TestOrchestrator_Run_InconclusiveThenPass(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot, healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{inconclusiveSnapshot, healthySnapshot},
	}
	o, r := newTestOrchestrator(t, metrics, canaryExec, lbExec, 2, time.Millisecond)

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StatePromoted {
		t.Fatalf("FinalState = %s, want Promoted (retry-then-pass should still succeed)", result.FinalState)
	}
	if r.TrafficPct != 100 {
		t.Errorf("TrafficPct = %d, want 100", r.TrafficPct)
	}
}

func TestOrchestrator_Run_InconclusiveExhaustsRetries_FailsSafe(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{inconclusiveSnapshot}, // always inconclusive
	}
	o, r := newTestOrchestrator(t, metrics, canaryExec, lbExec, 2, time.Millisecond)

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StateRolledBack {
		t.Errorf("FinalState = %s, want RolledBack (exhausted retries should fail safe)", result.FinalState)
	}
	if len(metrics.calls) != 4 { // 2 attempts * (stable + canary)
		t.Errorf("metrics calls = %d, want 4 (2 attempts before failing safe)", len(metrics.calls))
	}
	_ = r
}

func TestOrchestrator_Run_CanaryDeployFails(t *testing.T) {
	canaryExec := &fakeExecutor{
		runFunc: func(command string) (transport.Result, error) {
			if strings.Contains(command, "restart") {
				return transport.Result{ExitCode: 1, Stderr: "start request repeated too quickly"}, nil
			}
			return transport.Result{ExitCode: 0}, nil
		},
	}
	lbExec := &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{healthySnapshot},
	}
	o, r := newTestOrchestrator(t, metrics, canaryExec, lbExec, 3, time.Millisecond)

	_, err := o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when canary deploy fails, got nil")
	}
	if len(lbExec.calls) != 0 {
		t.Errorf("LB should never be touched if the canary deploy fails, calls: %v", lbExec.calls)
	}
	if r.State != rollout.StateProgressing {
		t.Errorf("rollout State = %s, want Progressing (should not advance past a failed deploy)", r.State)
	}
}

func TestOrchestrator_Run_ContextCancelledDuringAnalysisWait(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{inconclusiveSnapshot}, // always inconclusive, forces a wait
	}
	o, _ := newTestOrchestrator(t, metrics, canaryExec, lbExec, 5, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := o.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}
}

func TestOrchestrator_Run_WarmupDelayIsRespected(t *testing.T) {
	canaryExec, lbExec := &fakeExecutor{}, &fakeExecutor{}
	metrics := &fakeMetricsProvider{
		stableSnaps: []analysis.MetricSnapshot{healthySnapshot},
		canarySnaps: []analysis.MetricSnapshot{healthySnapshot},
	}
	canaryCfg, priorCfg := testUnitConfigs()
	r := rollout.NewRollout("checkout-svc", "v2.0.0", "v1.9.0")

	o, err := NewOrchestrator(Config{
		Rollout:                 r,
		CanaryDeployer:          deploy.NewDeployer(canaryExec),
		CanaryUnitConfig:        canaryCfg,
		PriorUnitConfig:         priorCfg,
		LB:                      loadbalancer.NewController(lbExec, "/etc/nginx/conf.d/canopy-upstream.conf"),
		Upstream:                testUpstream(),
		Analyzer:                analysis.NewAnalyzer(testThresholds()),
		Metrics:                 metrics,
		AnalysisInterval:        time.Millisecond,
		MaxInconclusiveAttempts: 3,
		WarmupDelay:             100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	start := time.Now()
	result, err := o.Run(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalState != rollout.StatePromoted {
		t.Fatalf("FinalState = %s, want Promoted", result.FinalState)
	}
	// 4 traffic steps, each preceded by a 100ms warmup wait.
	minExpected := 4 * 100 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("elapsed = %v, want at least %v (warmup delay should apply before each step's analysis)", elapsed, minExpected)
	}
}
