package analysis

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusProvider implements MetricsProvider against a real Prometheus
// server, querying error rate, p95 latency, and sample count for a given
// target label over a fixed lookback window.
type PrometheusProvider struct {
	api promv1.API
}

// NewPrometheusProvider connects to the Prometheus server at addr (e.g.
// "http://localhost:9090"). It does not perform a network call itself;
// connectivity is verified on first Snapshot call.
func NewPrometheusProvider(addr string) (*PrometheusProvider, error) {
	client, err := api.NewClient(api.Config{Address: addr})
	if err != nil {
		return nil, fmt.Errorf("analysis: create prometheus client: %w", err)
	}
	return &PrometheusProvider{api: promv1.NewAPI(client)}, nil
}

// lookbackWindow is the PromQL range used for all queries below. 1 minute
// balances responsiveness (canary steps shouldn't wait too long) against
// stability (too short a window makes error-rate ratios noisy for
// low-traffic services).
const lookbackWindow = "1m"

// Snapshot queries Prometheus for the given target's ("stable" or
// "canary") current error rate, p95 latency, and request sample count.
// It expects the application to expose Prometheus metrics matching:
//
//	http_requests_total{target="<target>",status="..."}
//	http_request_duration_seconds_bucket{target="<target>",le="..."}

func (p *PrometheusProvider) Snapshot(ctx context.Context, target string) (MetricSnapshot, error) {
	total, err := p.scalarQuery(ctx, fmt.Sprintf(
		`sum(increase(http_requests_total{target=%q}[%s]))`, target, lookbackWindow))
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("analysis: query total requests: %w", err)
	}

	errors, err := p.scalarQuery(ctx, fmt.Sprintf(
		`sum(increase(http_requests_total{target=%q,status=~"5.."}[%s]))`, target, lookbackWindow))
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("analysis: query error requests: %w", err)
	}

	p95, err := p.scalarQuery(ctx, fmt.Sprintf(
		`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{target=%q}[%s])) by (le)) * 1000`,
		target, lookbackWindow))
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("analysis: query p95 latency: %w", err)
	}

	var errorRate float64
	if total > 0 {
		errorRate = errors / total
	}

	return MetricSnapshot{
		ErrorRate:    errorRate,
		P95LatencyMs: p95,
		SampleCount:  int64(total),
	}, nil
}

// scalarQuery runs a PromQL instant query expected to return a single
// scalar value, returning 0 if the result set is empty (e.g. no traffic
// yet in the lookback window — not an error condition).
func (p *PrometheusProvider) scalarQuery(ctx context.Context, query string) (float64, error) {
	result, warnings, err := p.api.Query(ctx, query, timeNow())
	if err != nil {
		return 0, err
	}
	for _, w := range warnings {
		fmt.Println("prometheus warning:", w)
	}

	vector, ok := result.(model.Vector)
	if !ok || len(vector) == 0 {
		return 0, nil
	}
	return float64(vector[0].Value), nil
}
