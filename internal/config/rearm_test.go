package config

import (
	"strings"
	"testing"
)

func TestRearmBackendConfig(t *testing.T) {
	cfg := defaults()
	if cfg.RearmBackend != "decypharr" || !strings.Contains(DefaultConfigYAML, "rearm_backend: decypharr") {
		t.Fatal("default must be Decypharr")
	}
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.RadarrAPIKey = "key"
	cfg.APIToken = "token"
	if err := validate(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REARM_BACKEND", "DECYPHARR")
	applyEnvOverrides(&cfg)
	if cfg.RearmBackend != "decypharr" {
		t.Fatal("environment override missing")
	}
	cfg.DecypharrURL = ""
	if err := validate(cfg); err == nil {
		t.Fatal("Decypharr backend requires URL")
	}
	cfg.RearmBackend = "unknown"
	if err := validate(cfg); err == nil {
		t.Fatal("invalid backend accepted")
	}
}
