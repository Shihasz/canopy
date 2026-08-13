// Package loadbalancer manages nginx upstream configuration to control
// traffic weighting between a stable and canary backend.
package loadbalancer

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/Shihasz/canopy/internal/transport"
)

const upstreamTemplate = `# Managed by canopy. Do not edit by hand.
upstream {{.UpstreamName}} {
	server {{.StableAddr}} weight={{.StableWeight}};
	server {{.CanaryAddr}} weight={{.CanaryWeight}};
}
`

// UpstreamConfig describes one nginx upstream block splitting traffic
// between a stable and canary backend.
type UpstreamConfig struct {
	UpstreamName string
	StableAddr   string // e.g. "10.0.0.5:8080"
	CanaryAddr   string // e.g. "10.0.0.6:8080"
	CanaryPct    int    // 0-100, target % of traffic to the canary
}

// weights converts CanaryPct into a pair of small integer nginx weights.
// nginx weight= is relative, not literal percent, so we reduce the
// percentage to a ratio out of 100 directly (e.g. 10% -> weight 10:90).
func (c UpstreamConfig) weights() (stableWeight, canaryWeight int) {
	canaryWeight = c.CanaryPct
	stableWeight = 100 - c.CanaryPct
	// nginx requires weight >= 1 on every server line, so a 0% or 100%
	// split still needs the "off" side to have a token weight of 1..
	if canaryWeight == 0 {
		canaryWeight = 1
	}
	if stableWeight == 0 {
		stableWeight = 1
	}
	return stableWeight, canaryWeight
}

// RenderUpstream produces the nginx upstream block for cfg.
func RenderUpstream(cfg UpstreamConfig) (string, error) {
	if cfg.CanaryPct < 0 || cfg.CanaryPct > 100 {
		return "", fmt.Errorf("loadbalancer: CanaryPct must be 0-100, got %d", cfg.CanaryPct)
	}
	stableWeight, canaryWeight := cfg.weights()

	tmpl, err := template.New("upstream").Parse(upstreamTemplate)
	if err != nil {
		return "", fmt.Errorf("loadbalancer: parse upstream template: %w", err)
	}

	data := struct {
		UpstreamName string
		StableAddr   string
		CanaryAddr   string
		StableWeight int
		CanaryWeight int
	}{cfg.UpstreamName, cfg.StableAddr, cfg.CanaryAddr, stableWeight, canaryWeight}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("loadbalancer: render upstream template: %w", err)
	}
	return buf.String(), nil
}

// Controller pushes upstream configs to the nginx host and reloads it.
type Controller struct {
	exec       transport.Executor
	configPath string // e.g. /etc/nginx/conf.d/canopy-upstream.conf
}

// NewController wraps an Executor already connected to the nginx host.
func NewController(exec transport.Executor, configPath string) *Controller {
	return &Controller{exec: exec, configPath: configPath}
}

// SetTraffic renders cfg, writes it to configPath on the remote host,
// validates the new nginx config, and reloads nginx so it takes effect
// without dropping in-flight connections. If validation fails, the old
// config is left untouched — nginx -t is checked before nginx -s reload.
func (c *Controller) SetTraffic(ctx context.Context, cfg UpstreamConfig) error {
	content, err := RenderUpstream(cfg)
	if err != nil {
		return err
	}

	writeCmd := fmt.Sprintf("sudo tee %s > /dev/null <<'CANOPY_EOF'\n%s\nCANOPY_EOF", c.configPath, content)
	if err := c.runOK(ctx, writeCmd, "write upstream config"); err != nil {
		return err
	}

	if err := c.runOK(ctx, "sudo nginx -t", "validate nginx config"); err != nil {
		return fmt.Errorf("loadbalancer: new config failed validation, not reloading: %w", err)
	}

	return c.runOK(ctx, "sudo nginx -s reload", "reload nginx")
}

func (c *Controller) runOK(ctx context.Context, command, action string) error {
	result, err := c.exec.Run(ctx, command)
	if err != nil {
		return fmt.Errorf("loadbalancer: %s: %w", action, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("loadbalancer: %s: exit %d: %s", action, result.ExitCode, result.Stderr)
	}
	return nil
}
