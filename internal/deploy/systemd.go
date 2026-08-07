// Package deploy manages application deployment on a single VM via
// systemd, driven over a transport.Executor (typically SSH).
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/Shihasz/canopy/internal/transport"
)

const unitTemplate = `[Unit]
Description={{.Description}}
After=network.target

[Service]
Type=simple
User={{.User}}
WorkingDirectory={{.WorkingDirectory}}
ExecStart={{.ExecStart}}
Restart={{.Restart}}
RestartSec=2

[Install]
WantedBy=multi-user.target
`

// UnitConfig holds the values needed to render a systemd unit file for
// one version of the application. ExecStart is expected to point at a
// version-specific path (e.g. /opt/app/releases/v2.0.0/app)."
type UnitConfig struct {
	ServiceName      string
	Description      string
	ExecStart        string
	WorkingDirectory string
	User             string
	Restart          string // e.g. "on-failure"
}

// RenderUnit produces the systemd unit file content for cfg.
func RenderUnit(cfg UnitConfig) (string, error) {
	tmpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return "", fmt.Errorf("deploy: parse unit template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("deploy: render unit template: %w", err)
	}
	return buf.String(), nil
}

// ServiceStatus is the simplified state of a systemd unit.
type ServiceStatus string

const (
	StatusActive   ServiceStatus = "active"
	StatusInactive ServiceStatus = "inactive"
	StatusFailed   ServiceStatus = "failed"
	StatusUnknown  ServiceStatus = "unknown"
)

// Deployer drives systemd on one remote host over an Executor.
type Deployer struct {
	exec transport.Executor
}

// NewDeployer wraps an Executor for systemd operations on that host.
func NewDeployer(exec transport.Executor) *Deployer {
	return &Deployer{exec: exec}
}

func (d *Deployer) unitPath(serviceName string) string {
	return fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
}

// InstallUnit writes unitContent to the remote host's systemd directory
// and reloads the systemd daemon so it's recognized.
func (d *Deployer) InstallUnit(ctx context.Context, serviceName, unitContent string) error {
	path := d.unitPath(serviceName)
	// Heredoc with a quoted delimiter ('CANOPY_EOF') prevents the remote
	// shell from expanding $VARS or `backticks` inside the unit content.
	cmd := fmt.Sprintf("sudo tee %s > /dev/null <<'CANOPY_EOF'\n%s\nCANOPY_EOF", path, unitContent)
	if err := d.runOK(ctx, cmd, "write unit file"); err != nil {
		return err
	}
	return d.runOK(ctx, "sudo systemctl daemon-reload", "daemon-reload")
}

func (d *Deployer) Start(ctx context.Context, serviceName string) error {
	return d.runOK(ctx, fmt.Sprintf("sudo systemctl start %s", serviceName), "start service")
}

func (d *Deployer) Stop(ctx context.Context, serviceName string) error {
	return d.runOK(ctx, fmt.Sprintf("sudo systemctl stop %s", serviceName), "stop service")
}

func (d *Deployer) Restart(ctx context.Context, serviceName string) error {
	return d.runOK(ctx, fmt.Sprintf("sudo systemctl restart %s", serviceName), "restart service")
}

// Status queries the current state of a systemd unit.
func (d *Deployer) Status(ctx context.Context, serviceName string) (ServiceStatus, error) {
	result, err := d.exec.Run(ctx, fmt.Sprintf("systemctl is-active %s", serviceName))
	if err != nil {
		return StatusUnknown, fmt.Errorf("deploy: query status of %s: %w", serviceName, err)
	}
	switch strings.TrimSpace(result.Stdout) {
	case "active":
		return StatusActive, nil
	case "inactive":
		return StatusInactive, nil
	case "failed":
		return StatusFailed, nil
	default:
		return StatusUnknown, nil
	}
}

// Deploy renders cfg into a unit file, installs it, and restarts the
// service so the new ExecStart takes effect.
func (d *Deployer) Deploy(ctx context.Context, cfg UnitConfig) error {
	unit, err := RenderUnit(cfg)
	if err != nil {
		return err
	}
	if err := d.InstallUnit(ctx, cfg.ServiceName, unit); err != nil {
		return err
	}
	return d.Restart(ctx, cfg.ServiceName)
}

// Rollback redeploys the service using cfg.
func (d *Deployer) Rollback(ctx context.Context, cfg UnitConfig) error {
	return d.Deploy(ctx, cfg)
}

// runOK runs command and turns a non-zero exit code into a Go error
// (including stderr for debugging). Used for commands where any failure
// is unexpected.
func (d *Deployer) runOK(ctx context.Context, command, action string) error {
	result, err := d.exec.Run(ctx, command)
	if err != nil {
		return fmt.Errorf("deploy: %s: %w", action, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("deploy: %s: exit %d: %s", action, result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}
