package analysis

import (
	"testing"
)

func defaultThresholds() Thresholds {
	return Thresholds{
		MaxErrorRate:      0.05, // 5%
		MaxErrorRateDelta: 0.02, // 2 percentage points over stable
		MaxP95LatencyMs:   500,
		MinSampleCount:    50,
	}
}

func TestAnalyzer_Evaluate_Pass(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	stable := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	canary := MetricSnapshot{ErrorRate: 0.015, P95LatencyMs: 220, SampleCount: 200}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictPass {
		t.Errorf("Verdict = %s, want Pass. Reasons: %v", report.Verdict, report.Reasons)
	}
}

func TestAnalyzer_Evaluate_FailsOnAbsoluteErrorRate(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	stable := MetricSnapshot{ErrorRate: 0.04, P95LatencyMs: 200, SampleCount: 1000}
	canary := MetricSnapshot{ErrorRate: 0.08, P95LatencyMs: 200, SampleCount: 200} // over 0.05 absolute

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictFail {
		t.Fatalf("Verdict = %s, want Fail", report.Verdict)
	}
	if len(report.Reasons) == 0 {
		t.Error("expected at least one reason")
	}
}

func TestAnalyzer_Evaluate_FailsOnRelativeErrorRateDelta(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	// Both under the 0.05 absolute ceiling, but canary is 3pp hotter than
	// stable, over the 0.02 allowed delta.
	stable := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	canary := MetricSnapshot{ErrorRate: 0.04, P95LatencyMs: 200, SampleCount: 200}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictFail {
		t.Fatalf("Verdict = %s, want Fail", report.Verdict)
	}
}

func TestAnalyzer_Evaluate_FailsOnLatency(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	stable := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	canary := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 900, SampleCount: 200}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictFail {
		t.Fatalf("Verdict = %s, want Fail", report.Verdict)
	}
}

func TestAnalyzer_Evaluate_CollectsMultipleReasons(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	// Fails the absolute error rate check, the relative delta check
	// (0.10 - 0.01 = 0.09, over the 0.02 allowed delta), AND latency,
	// all simultaneously — three independent reasons.
	stable := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	canary := MetricSnapshot{ErrorRate: 0.10, P95LatencyMs: 900, SampleCount: 200}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictFail {
		t.Fatalf("Verdict = %s, want Fail", report.Verdict)
	}
	if len(report.Reasons) != 3 {
		t.Errorf("expected 3 reasons (absolute error rate + relative delta + latency), got %d: %v", len(report.Reasons), report.Reasons)
	}
}

func TestAnalyzer_Evaluate_InconclusiveOnLowSampleCount(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	stable := MetricSnapshot{ErrorRate: 0.01, P95LatencyMs: 200, SampleCount: 1000}
	// Only 10 samples, below MinSampleCount of 50 — even though error
	canary := MetricSnapshot{ErrorRate: 0.0, P95LatencyMs: 50, SampleCount: 10}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictInconclusive {
		t.Fatalf("Verdict = %s, want Inconclusive", report.Verdict)
	}
}

func TestAnalyzer_Evaluate_BoundaryIsPass(t *testing.T) {
	a := NewAnalyzer(defaultThresholds())
	// stable set so that canary's delta (0.05 - 0.03 = 0.02) sits exactly
	// at MaxErrorRateDelta too, not just the absolute MaxErrorRate — both
	// boundaries need to be hit at once for this to be a true boundary test.
	stable := MetricSnapshot{ErrorRate: 0.03, P95LatencyMs: 200, SampleCount: 1000}
	// Exactly at the thresholds, not over them — should still pass.
	canary := MetricSnapshot{ErrorRate: 0.05, P95LatencyMs: 500, SampleCount: 200}

	report := a.Evaluate(stable, canary)
	if report.Verdict != VerdictPass {
		t.Errorf("Verdict = %s, want Pass (boundary values should pass, not fail). Reasons: %v", report.Verdict, report.Reasons)
	}
}
