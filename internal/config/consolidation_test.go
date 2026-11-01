package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentConfigExamplesAndEmbeddedDefault(t *testing.T) {
	paths := []string{"../../config.example.yaml", "../../config/rehydrator.example.yaml", "../../config/rehydrator.gateway.example.yaml", "../../config/rehydrator.controller.example.yaml"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			fc, err := readFileConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if !fc.Gateway.Enabled || fc.Gateway.RadarrCategory != "radarr" || fc.Gateway.SonarrCategory != "sonarr" {
				t.Fatal("invalid gateway defaults")
			}
		})
	}
	for _, path := range paths[:2] {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != DefaultConfigYAML {
			t.Fatal("embedded/default examples drifted", path, err)
		}
	}
}
func TestRetiredOrUnknownYAMLRejected(t *testing.T) {
	for _, config := range []string{"decypharr:\n  url: http://old\n", "cloud_usenet:\n  enabled: true\n", "prune_enabled: false\n", "rearm_backend: torbox\n", "torbox_health:\n  enabled: false\n", "file_health:\n  interval_seconds: 300\n", "gateway:\n  active_pol_interval_seconds: 30\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readFileConfig(path); err == nil || !strings.Contains(err.Error(), "docs/configuration.md") {
			t.Fatal("retired/unknown config accepted", config, err)
		}
	}
}
func TestRetiredEnvironmentRejectedWithoutSecretExposure(t *testing.T) {
	for _, name := range []string{"DECYPHARR_API_TOKEN", "CLOUD_USENET_API_KEY", "PRUNE_ENABLED", "FILE_HEALTH_ENABLED", "REARM_BACKEND"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "sensitive-test-value")
			err := rejectRetiredEnvironment()
			if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "sensitive-test-value") {
				t.Fatal(err)
			}
		})
	}
}
func TestGatewayCategoryOverrides(t *testing.T) {
	cfg := defaults()
	t.Setenv("GATEWAY_RADARR_CATEGORY", "movies")
	t.Setenv("GATEWAY_SONARR_CATEGORY", "television")
	applyEnvOverrides(&cfg)
	if cfg.Gateway.RadarrCategory != "movies" || cfg.Gateway.SonarrCategory != "television" {
		t.Fatal(cfg.Gateway)
	}
}
