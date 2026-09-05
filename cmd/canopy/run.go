package main

import (
	"context"
	"fmt"

	"github.com/Shihasz/canopy/internal/analysis"
	"github.com/Shihasz/canopy/internal/config"
	"github.com/Shihasz/canopy/internal/deploy"
	"github.com/Shihasz/canopy/internal/loadbalancer"
	"github.com/Shihasz/canopy/internal/orchestrator"
	"github.com/Shihasz/canopy/internal/rollout"
	"github.com/Shihasz/canopy/internal/transport"
)

// runDeploy wires config into a real orchestrator and runs one rollout
// to completion. It opens two SSH connections: one to the canary host
// (for systemd deploy) and one to the load balancer host.
func runDeploy(ctx context.Context, cfg *config.Config, newVersion, priorVersion string) error {
	signer, err := transport.LoadSigner(cfg.SSH.PrivateKey)
	if err != nil {
		return err
	}

	canaryConn, err := transport.NewSSHExecutor(transport.Config{
		Host: cfg.Canary.Host, Port: cfg.SSH.Port, User: cfg.SSH.User, Signer: signer,
	})
	if err != nil {
		return fmt.Errorf("connect to canary host: %w", err)
	}
	defer canaryConn.Close()

	lbConn, err := transport.NewSSHExecutor(transport.Config{
		Host: cfg.LoadBalancer.Host, Port: cfg.SSH.Port, User: cfg.SSH.User, Signer: signer,
	})
	if err != nil {
		return fmt.Errorf("connect to load balancer host: %w", err)
	}
	defer lbConn.Close()

	canaryUnit := deploy.UnitConfig{
		ServiceName:      cfg.ServiceName,
		Description:      fmt.Sprintf("%s (managed by canopy)", cfg.ServiceName),
		ExecStart:        fmt.Sprintf(cfg.Canary.ExecStartFormat, newVersion),
		WorkingDirectory: cfg.Canary.WorkingDirectory,
		User:             cfg.Canary.User,
		Restart:          "on-failure",
	}
	priorUnit := canaryUnit
	priorUnit.ExecStart = fmt.Sprintf(cfg.Canary.ExecStartFormat, priorVersion)

	promMetrics, err := analysis.NewPrometheusProvider(cfg.Prometheus.URL)
	if err != nil {
		return fmt.Errorf("connect to prometheus: %w", err)
	}

	o, err := orchestrator.NewOrchestrator(orchestrator.Config{
		Rollout:          rollout.NewRollout(cfg.ServiceName, newVersion, priorVersion),
		CanaryDeployer:   deploy.NewDeployer(canaryConn),
		CanaryUnitConfig: canaryUnit,
		PriorUnitConfig:  priorUnit,
		LB:               loadbalancer.NewController(lbConn, cfg.LoadBalancer.ConfigPath),
		Upstream: loadbalancer.UpstreamConfig{
			UpstreamName: cfg.LoadBalancer.UpstreamName,
			StableAddr:   cfg.Stable.AppAddr,
			CanaryAddr:   cfg.Canary.AppAddr,
		},
		Analyzer: analysis.NewAnalyzer(analysis.Thresholds{
			MaxErrorRate:      cfg.Analysis.MaxErrorRate,
			MaxErrorRateDelta: cfg.Analysis.MaxErrorRateDelta,
			MaxP95LatencyMs:   cfg.Analysis.MaxP95LatencyMs,
			MinSampleCount:    cfg.Analysis.MinSampleCount,
		}),
		Metrics:                 promMetrics,
		WarmupDelay:             cfg.Analysis.WarmupDelay,
		AnalysisInterval:        cfg.Analysis.Interval,
		MaxInconclusiveAttempts: cfg.Analysis.MaxInconclusive,
	})
	if err != nil {
		return err
	}

	result, err := o.Run(ctx)
	if err != nil {
		return fmt.Errorf("rollout failed: %w", err)
	}

	fmt.Printf("Rollout finished: %s\n", result.FinalState)
	for _, reason := range result.LastReport.Reasons {
		fmt.Println(" -", reason)
	}
	return nil
}
