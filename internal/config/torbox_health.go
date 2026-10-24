package config

// TorBoxHealthConfig is an opt-in read-only exception to Decypharr ownership.
type TorBoxHealthConfig struct {
	Enabled         bool   `yaml:"enabled"`
	APIKey          string `yaml:"api_key"`
	IntervalSeconds int    `yaml:"interval_seconds"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
}

func DefaultTorBoxHealth() TorBoxHealthConfig {
	return TorBoxHealthConfig{IntervalSeconds: 900, TimeoutSeconds: 20}
}
