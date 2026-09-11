package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validYAML = `
serviceName: checkout-svc
ssh:
  user: deploy
  privateKey: /home/deploy/.ssh/id_ed25519
canary:
  host: canary.internal
  appAddr: 10.0.0.6:8080
  workingDirectory: /opt/checkout-svc/releases/current
  execStartFormat: /opt/checkout-svc/releases/%s/app
  user: appuser
stable:
  host: stable.internal
  appAddr: 10.0.0.5:8080
loadBalancer:
  host: lb.internal
  configPath: /etc/nginx/conf.d/canopy-upstream.conf
  upstreamName: checkout_svc
prometheus:
  url: http://prometheus.internal:9090
analysis:
  maxErrorRate: 0.05
  maxErrorRateDelta: 0.02
  maxP95LatencyMs: 500
  minSampleCount: 50
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "canopy.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoad_Valid(t *testing.T) {
	path := writeTempConfig(t, validYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServiceName != "checkout-svc" {
		t.Errorf("ServiceName = %q, want checkout-svc", cfg.ServiceName)
	}
	if cfg.Canary.Host != "canary.internal" {
		t.Errorf("Canary.Host = %q, want canary.internal", cfg.Canary.Host)
	}
	// Defaults should be applied by Validate (called from Load).
	if cfg.SSH.Port != 22 {
		t.Errorf("SSH.Port default = %d, want 22", cfg.SSH.Port)
	}
	if cfg.Analysis.MaxInconclusive != 3 {
		t.Errorf("Analysis.MaxInconclusive default = %d, want 3", cfg.Analysis.MaxInconclusive)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/canopy.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_MissingRequiredField(t *testing.T) {
	// Same as validYAML but with serviceName removed.
	badYAML := `
ssh:
  user: deploy
  privateKey: /home/deploy/.ssh/id_ed25519
canary:
  host: canary.internal
  appAddr: 10.0.0.6:8080
  execStartFormat: /opt/checkout-svc/releases/%s/app
stable:
  host: stable.internal
  appAddr: 10.0.0.5:8080
loadBalancer:
  host: lb.internal
  configPath: /etc/nginx/conf.d/canopy-upstream.conf
prometheus:
  url: http://prometheus.internal:9090
`
	path := writeTempConfig(t, badYAML)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing serviceName, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempConfig(t, "not: valid: yaml: [unclosed")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_LoadBalancerPortDefaultsToSSHPort(t *testing.T) {
	path := writeTempConfig(t, validYAML) // validYAML never sets loadBalancer.port
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LoadBalancer.Port != cfg.SSH.Port {
		t.Errorf("LoadBalancer.Port = %d, want it to default to SSH.Port (%d)", cfg.LoadBalancer.Port, cfg.SSH.Port)
	}
}

const yamlWithExplicitLBPort = `
serviceName: checkout-svc
ssh:
  user: deploy
  privateKey: /home/deploy/.ssh/id_ed25519
canary:
  host: canary.internal
  appAddr: 10.0.0.6:8080
  execStartFormat: /opt/checkout-svc/releases/%s/app
stable:
  host: stable.internal
  appAddr: 10.0.0.5:8080
loadBalancer:
  host: lb.internal
  port: 2203
  configPath: /etc/nginx/conf.d/canopy-upstream.conf
prometheus:
  url: http://prometheus.internal:9090
`

func TestLoad_LoadBalancerPortExplicitOverride(t *testing.T) {
	path := writeTempConfig(t, yamlWithExplicitLBPort)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LoadBalancer.Port != 2203 {
		t.Errorf("LoadBalancer.Port = %d, want 2203 (explicit value should not be overridden by default)", cfg.LoadBalancer.Port)
	}
	if cfg.SSH.Port != 22 {
		t.Errorf("SSH.Port = %d, want 22 (default), independent of LoadBalancer.Port", cfg.SSH.Port)
	}
}
