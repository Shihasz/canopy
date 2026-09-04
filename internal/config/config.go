// Package config loads canopy's rollout configuration from a YAML file.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full on-disk configuration for one service's rollouts.
type Config struct {
	ServiceName string `yaml:"serviceName"`

	SSH struct {
		User       string `yaml:"user"`
		PrivateKey string `yaml:"privateKey"` // path to the SSH private key file
		Port       int    `yaml:"port"`
	} `yaml:"ssh"`

	Canary struct {
		Host             string `yaml:"host"`
		AppAddr          string `yaml:"appAddr"` // host:port the app listens on
		WorkingDirectory string `yaml:"workingDirectory"`
		ExecStartFormat  string `yaml:"execStartFormat"` // e.g. "/opt/app/releases/%s/app", %s is replaced with the version
		User             string `yaml:"user"`            // unix user the systemd service runs as
	} `yaml:"canary"`

	Stable struct {
		Host    string `yaml:"host"`
		AppAddr string `yaml:"appAddr"`
	} `yaml:"stable"`

	LoadBalancer struct {
		Host         string `yaml:"host"`
		ConfigPath   string `yaml:"configPath"`
		UpstreamName string `yaml:"upstreamName"`
	} `yaml:"loadBalancer"`

	Prometheus struct {
		URL string `yaml:"url"`
	} `yaml:"prometheus"`

	Analysis struct {
		MaxErrorRate      float64       `yaml:"maxErrorRate"`
		MaxErrorRateDelta float64       `yaml:"maxErrorRateDelta"`
		MaxP95LatencyMs   float64       `yaml:"maxP95LatencyMs"`
		MinSampleCount    int64         `yaml:"minSampleCount"`
		Interval          time.Duration `yaml:"interval"`
		MaxInconclusive   int           `yaml:"maxInconclusive"`
	} `yaml:"analysis"`
}

// Load reads and parses a Config from the YAML file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %q: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks that required fields are present. It does not attempt
// to validate reachability of hosts — that's discovered at connect time.
func (c *Config) Validate() error {
	var missing []string
	if c.ServiceName == "" {
		missing = append(missing, "serviceName")
	}
	if c.SSH.User == "" {
		missing = append(missing, "ssh.user")
	}
	if c.SSH.PrivateKey == "" {
		missing = append(missing, "ssh.privateKey")
	}
	if c.Canary.Host == "" {
		missing = append(missing, "canary.host")
	}
	if c.Canary.AppAddr == "" {
		missing = append(missing, "canary.appAddr")
	}
	if c.Canary.ExecStartFormat == "" {
		missing = append(missing, "canary.execStartFormat")
	}
	if c.Stable.Host == "" {
		missing = append(missing, "stable.host")
	}
	if c.Stable.AppAddr == "" {
		missing = append(missing, "stable.appAddr")
	}
	if c.LoadBalancer.Host == "" {
		missing = append(missing, "loadBalancer.host")
	}
	if c.LoadBalancer.ConfigPath == "" {
		missing = append(missing, "loadBalancer.configPath")
	}
	if c.Prometheus.URL == "" {
		missing = append(missing, "prometheus.url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %v", missing)
	}
	if c.SSH.Port == 0 {
		c.SSH.Port = 22
	}
	if c.Analysis.Interval == 0 {
		c.Analysis.Interval = 5 * time.Second
	}
	if c.Analysis.MaxInconclusive == 0 {
		c.Analysis.MaxInconclusive = 3
	}
	return nil
}
