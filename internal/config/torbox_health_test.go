package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTorBoxHealthConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("torbox_health:\n  enabled: true\n  api_key: secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaults()
	applyFileConfig(&cfg, fc)
	if !cfg.TorBoxHealth.Enabled || cfg.TorBoxHealth.IntervalSeconds != 900 {
		t.Fatal("config defaults missing")
	}
	t.Setenv("TORBOX_HEALTH_API_KEY", "override")
	applyEnvOverrides(&cfg)
	if cfg.TorBoxHealth.APIKey != "override" {
		t.Fatal("key override missing")
	}
	cfg.TorBoxHealth.APIKey = ""
	if err := validate(cfg); err == nil {
		t.Fatal("enabled health accepted missing key")
	}
}
