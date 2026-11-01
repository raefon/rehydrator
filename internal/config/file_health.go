package config

// FileHealthConfig retains the same-mount canary used for confirmed missing-file updates.
// Gateway polling owns provider availability and recovery.
type FileHealthConfig struct {
	CanaryPath string `yaml:"canary_path"`
}

func DefaultFileHealth() FileHealthConfig { return FileHealthConfig{} }
