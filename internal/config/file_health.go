package config

// FileHealthConfig controls a rolling availability sweep, not provider keepalive.
type FileHealthConfig struct {
	CanaryPath           string `yaml:"canary_path"`
	Enabled              bool   `yaml:"enabled"`
	IntervalSeconds      int    `yaml:"interval_seconds"`
	BatchSize            int    `yaml:"batch_size"`
	FailureThreshold     int    `yaml:"failure_threshold"`
	MaxRearmsPerRun      int    `yaml:"max_rearms_per_run"`
	ProbeTimeoutSeconds  int    `yaml:"probe_timeout_seconds"`
	OutageFailurePercent int    `yaml:"outage_failure_percent"`
}

func DefaultFileHealth() FileHealthConfig {
	return FileHealthConfig{Enabled: true, IntervalSeconds: 300, BatchSize: 20, FailureThreshold: 3, MaxRearmsPerRun: 2, ProbeTimeoutSeconds: 5, OutageFailurePercent: 50}
}
