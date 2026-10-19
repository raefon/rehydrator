package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJellyfinConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("jellyfin:\n  enabled: true\n  api_key: secret\n  refresh_after_import: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaults()
	applyFileConfig(&cfg, fc)
	if !cfg.Jellyfin.Enabled || cfg.Jellyfin.RefreshAfterImport || !cfg.Jellyfin.RefreshAfterVisibility || cfg.Jellyfin.TimeoutSeconds != 20 {
		t.Fatalf("unexpected config: %+v", cfg.Jellyfin)
	}
	t.Setenv("JELLYFIN_URL", "http://localhost:8096/jellyfin")
	t.Setenv("JELLYFIN_REFRESH_AFTER_IMPORT", "true")
	applyEnvOverrides(&cfg)
	if cfg.Jellyfin.URL != "http://localhost:8096/jellyfin" || !cfg.Jellyfin.RefreshAfterImport {
		t.Fatalf("environment overrides missing: %+v", cfg.Jellyfin)
	}
}
