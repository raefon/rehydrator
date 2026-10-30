package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHealthConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("file_health:\n  enabled: false\n  recover_archived: true\n  batch_size: 30\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaults()
	applyFileConfig(&cfg, fc)
	if !cfg.FileHealth.RecoverArchived || cfg.FileHealth.Enabled || cfg.FileHealth.BatchSize != 30 || cfg.FileHealth.FailureThreshold != 3 {
		t.Fatalf("config=%+v", cfg.FileHealth)
	}
	t.Setenv("FILE_HEALTH_RECOVER_ARCHIVED", "false")
	t.Setenv("FILE_HEALTH_ENABLED", "true")
	t.Setenv("FILE_HEALTH_CANARY_PATH", "/mount/canary")
	applyEnvOverrides(&cfg)
	if cfg.FileHealth.RecoverArchived || !cfg.FileHealth.Enabled || cfg.FileHealth.CanaryPath != "/mount/canary" {
		t.Fatal("environment overrides missing")
	}
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.RadarrAPIKey = "key"
	cfg.APIToken = "token"
	if err := validate(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.FileHealth.FailureThreshold = 1
	if err := validate(cfg); err == nil {
		t.Fatal("single failure confirmation accepted")
	}
}
