// Package orchestrator drives a single canary rollout end-to-end by
// wiring together deployment, traffic shifting, and analysis.
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/Shihasz/canopy/internal/analysis"
	"github.com/Shihasz/canopy/internal/deploy"
	"github.com/Shihasz/canopy/internal/loadbalancer"
	"github.com/Shihasz/canopy/internal/rollout"
)

// Config wires together everything one rollout needs. CanaryDeployer and
// LB are expected to already be connected to their respective target
// hosts (canary VM, nginx host) — this package doesn't manage SSH
// connections itself, only drives the higher-level workflow over them.
type Config struct {
	Rollout *rollout.Rollout

	CanaryDeployer   *deploy.Deployer
	CanaryUnitConfig deploy.UnitConfig // new version, deployed to the canary host
	PriorUnitConfig  deploy.UnitConfig // prior version, redeployed to the canary host on rollback

	LB       *loadbalancer.Controller
	Upstream loadbalancer.UpstreamConfig // CanaryPct is overwritten by the orchestrator at each step

	Analyzer *analysis.Analyzer
	Metrics  analysis.MetricsProvider

	// AnalysisInterval is how long to wait between retry attempts when a
	// verdict comes back Inconclusive. Defaults to 5s if unset.
	AnalysisInterval time.Duration

	// WarmupDelay is how long to wait after shifting traffic to a new
	// percentage before running the first analysis pass at that step.
	// This gives the metrics backend time to scrape fresh data reflecting
	// traffic under the new split, rather than evaluating stale samples
	// left over from before the shift. Defaults to 0 (no wait) — safe for
	// tests using fakes with instantaneous metrics, but real deployments
	// should set this to at least one scrape interval.
	WarmupDelay time.Duration

	// MaxInconclusiveAttempts is how many times to retry an Inconclusive
	// verdict before failing safe (treating it as a Fail). Defaults to 3.
	MaxInconclusiveAttempts int
}

// Result summarizes how a rollout finished.
type Result struct {
	FinalState rollout.State
	LastReport analysis.Report
}

// Orchestrator drives one rollout through its full lifecycle.
type Orchestrator struct {
	cfg Config
}

// NewOrchestrator validates cfg and applies defaults for unset optional
// fields.
func NewOrchestrator(cfg Config) (*Orchestrator, error) {
	if cfg.Rollout == nil {
		return nil, fmt.Errorf("orchestrator: Config.Rollout is required")
	}
	if cfg.CanaryDeployer == nil {
		return nil, fmt.Errorf("orchestrator: Config.CanaryDeployer is required")
	}
	if cfg.LB == nil {
		return nil, fmt.Errorf("orchestrator: Config.LB is required")
	}
	if cfg.Analyzer == nil {
		return nil, fmt.Errorf("orchestrator: Config.Analyzer is required")
	}
	if cfg.Metrics == nil {
		return nil, fmt.Errorf("orchestrator: Config.Metrics is required")
	}
	if cfg.AnalysisInterval <= 0 {
		cfg.AnalysisInterval = 5 * time.Second
	}
	if cfg.MaxInconclusiveAttempts <= 0 {
		cfg.MaxInconclusiveAttempts = 3
	}
	return &Orchestrator{cfg: cfg}, nil
}

// Run drives the rollout from its current state through to a terminal
// state (Promoted or RolledBack), or returns an error if a step along the
// way fails unrecoverably (e.g. a transport error talking to a VM).
func (o *Orchestrator) Run(ctx context.Context) (*Result, error) {
	r := o.cfg.Rollout

	if err := r.Transition(rollout.StateProgressing); err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}
	if err := o.cfg.CanaryDeployer.Deploy(ctx, o.cfg.CanaryUnitConfig); err != nil {
		return nil, fmt.Errorf("orchestrator: deploy canary: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		step, err := r.NextTrafficStep()
		if err != nil {
			return nil, fmt.Errorf("orchestrator: unexpected state, no next traffic step: %w", err)
		}

		upstream := o.cfg.Upstream
		upstream.CanaryPct = step
		if err := o.cfg.LB.SetTraffic(ctx, upstream); err != nil {
			return nil, fmt.Errorf("orchestrator: shift traffic to %d%%: %w", step, err)
		}
		r.TrafficPct = step

		if err := sleepCtx(ctx, o.cfg.WarmupDelay); err != nil {
			return nil, err
		}

		if err := r.Transition(rollout.StateAnalyzing); err != nil {
			return nil, fmt.Errorf("orchestrator: %w", err)
		}

		verdict, report, err := o.analyze(ctx)
		if err != nil {
			return nil, err
		}

		switch verdict {
		case analysis.VerdictFail:
			return o.rollback(ctx, report)
		case analysis.VerdictPass:
			if step == 100 {
				return o.promote(report)
			}
			if err := r.Transition(rollout.StateProgressing); err != nil {
				return nil, fmt.Errorf("orchestrator: %w", err)
			}
		default:
			// analyze() resolves Inconclusive internally (retry, then fail
			// safe) and never returns it directly — this is a defensive
			// guard against that invariant being broken later, not a path
			// we expect to hit.
			return nil, fmt.Errorf("orchestrator: unexpected verdict %q from analyze", verdict)
		}
	}
}

// analyze fetches metrics and evaluates them, retrying while the verdict
// is Inconclusive. If still Inconclusive after MaxInconclusiveAttempts,
// it fails safe by returning VerdictFail rather than leaving the rollout
// stuck or risking a promotion on insufficient data.
func (o *Orchestrator) analyze(ctx context.Context) (analysis.Verdict, analysis.Report, error) {
	var lastReport analysis.Report

	for attempt := 0; attempt < o.cfg.MaxInconclusiveAttempts; attempt++ {
		stableSnap, err := o.cfg.Metrics.Snapshot(ctx, "stable")
		if err != nil {
			return "", analysis.Report{}, fmt.Errorf("orchestrator: fetch stable metrics: %w", err)
		}
		canarySnap, err := o.cfg.Metrics.Snapshot(ctx, "canary")
		if err != nil {
			return "", analysis.Report{}, fmt.Errorf("orchestrator: fetch canary metrics: %w", err)
		}

		lastReport = o.cfg.Analyzer.Evaluate(stableSnap, canarySnap)
		if lastReport.Verdict != analysis.VerdictInconclusive {
			return lastReport.Verdict, lastReport, nil
		}

		if attempt < o.cfg.MaxInconclusiveAttempts-1 {
			if err := sleepCtx(ctx, o.cfg.AnalysisInterval); err != nil {
				return "", analysis.Report{}, err
			}
		}
	}

	return analysis.VerdictFail, lastReport, nil
}

// rollback redeploys the canary host to the prior version and shifts all
// traffic back to stable.
func (o *Orchestrator) rollback(ctx context.Context, report analysis.Report) (*Result, error) {
	if err := o.cfg.CanaryDeployer.Rollback(ctx, o.cfg.PriorUnitConfig); err != nil {
		return nil, fmt.Errorf("orchestrator: rollback deploy: %w", err)
	}

	upstream := o.cfg.Upstream
	upstream.CanaryPct = 0
	if err := o.cfg.LB.SetTraffic(ctx, upstream); err != nil {
		return nil, fmt.Errorf("orchestrator: rollback traffic shift: %w", err)
	}
	o.cfg.Rollout.TrafficPct = 0

	if err := o.cfg.Rollout.Transition(rollout.StateRolledBack); err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}
	return &Result{FinalState: rollout.StateRolledBack, LastReport: report}, nil
}

// promote finalizes the rollout. Traffic is already at 100% from the
// last progressive step by the time this is called, so promotion only
// needs to close out the state machine.
func (o *Orchestrator) promote(report analysis.Report) (*Result, error) {
	if err := o.cfg.Rollout.Transition(rollout.StatePromoted); err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}
	return &Result{FinalState: rollout.StatePromoted, LastReport: report}, nil
}

// sleepCtx waits for d, or returns ctx.Err() early if ctx is cancelled
// first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
