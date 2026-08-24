// Package analysis compares canary and stable metrics against configured
// thresholds to decide whether a rollout should progress, hold, or roll back.
package analysis

import (
	"context"
	"fmt"
)

// To prevent floating point precision issue
const epsilon = 1e-9

// MetricSnapshot is a point-in-time read of the health signals we care
// about for one target (stable or canary).
type MetricSnapshot struct {
	ErrorRate    float64 // fraction of requests that errored, 0.0-1.0
	P95LatencyMs float64 // 95th percentile latency in milliseconds
	SampleCount  int64   // number of requests the snapshot covers
}

// MetricsProvider fetches a MetricSnapshot for a named target (e.g.
// "canary" or "stable")
type MetricsProvider interface {
	Snapshot(ctx context.Context, target string) (MetricSnapshot, error)
}

// Thresholds define the pass/fail bar for a canary analysis.
type Thresholds struct {
	// MaxErrorRate is the absolute ceiling on canary error rate, regardless
	// of how stable is performing. e.g. 0.05 = 5%.
	MaxErrorRate float64

	// MaxErrorRateDelta is how much higher the canary's error rate is
	// allowed to be than stable's, in absolute terms. e.g. 0.02 means
	// canary can run up to 2 percentage points hotter than stable.
	MaxErrorRateDelta float64

	// MaxP95LatencyMs is the absolute ceiling on canary p95 latency.
	MaxP95LatencyMs float64

	// MinSampleCount is the minimum number of requests a snapshot must
	// cover before we trust it enough to make a decision. Below this,
	// the verdict is Inconclusive rather than Pass or Fail.
	MinSampleCount int64
}

// Verdict is the outcome of one analysis pass.
type Verdict string

const (
	VerdictPass         Verdict = "Pass"
	VerdictFail         Verdict = "Fail"
	VerdictInconclusive Verdict = "Inconclusive"
)

// Report is the full result of an analysis pass: the verdict plus the
// specific reasons behind it, so callers can log or surface *why*.
type Report struct {
	Verdict Verdict
	Reasons []string
	Stable  MetricSnapshot
	Canary  MetricSnapshot
}

// Analyzer evaluates canary health against a fixed set of thresholds.
type Analyzer struct {
	thresholds Thresholds
}

// NewAnalyzer builds an Analyzer with the given thresholds.
func NewAnalyzer(t Thresholds) *Analyzer {
	return &Analyzer{thresholds: t}
}

// Evaluate compares stable and canary snapshots against the configured
// thresholds and returns a Report explaining the verdict.
//
// Order of checks: sample size first (an Inconclusive verdict short-circuits
// everything else, since a decision made on too little data isn't
// trustworthy either way), then the absolute and relative error-rate
// checks, then latency. All applicable failing checks are collected into
// Reasons rather than stopping at the first one, so a caller sees the
// full picture in one pass.
func (a *Analyzer) Evaluate(stable, canary MetricSnapshot) Report {
	report := Report{Stable: stable, Canary: canary}

	if canary.SampleCount < a.thresholds.MinSampleCount {
		report.Verdict = VerdictInconclusive
		report.Reasons = append(report.Reasons, fmt.Sprintf(
			"canary sample count %d below minimum %d", canary.SampleCount, a.thresholds.MinSampleCount))
		return report
	}

	var reasons []string

	if canary.ErrorRate > a.thresholds.MaxErrorRate+epsilon {
		reasons = append(reasons, fmt.Sprintf(
			"canary error rate %.4f exceeds absolute max %.4f", canary.ErrorRate, a.thresholds.MaxErrorRate))
	}

	delta := canary.ErrorRate - stable.ErrorRate
	if delta > a.thresholds.MaxErrorRateDelta+epsilon {
		reasons = append(reasons, fmt.Sprintf(
			"canary error rate %.4f exceeds stable %.4f by %.4f, over allowed delta %.4f",
			canary.ErrorRate, stable.ErrorRate, delta, a.thresholds.MaxErrorRateDelta))
	}

	if canary.P95LatencyMs > a.thresholds.MaxP95LatencyMs+epsilon {
		reasons = append(reasons, fmt.Sprintf(
			"canary p95 latency %.1fms exceeds max %.1fms", canary.P95LatencyMs, a.thresholds.MaxP95LatencyMs))
	}

	if len(reasons) > 0 {
		report.Verdict = VerdictFail
		report.Reasons = reasons
		return report
	}

	report.Verdict = VerdictPass
	report.Reasons = []string{"all metrics within thresholds"}
	return report
}
