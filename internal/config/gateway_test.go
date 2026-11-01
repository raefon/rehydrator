package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectGatewayConfiguration(t *testing.T) {
	cfg := defaults()
	file := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(file, []byte("gateway:\n  enabled: true\n  qb_username: arr\n  qb_password: secret\n  api_key: torbox\n  sab_api_key: sab\n  dav_username: dav\n  dav_password: secret\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	applyFileConfig(&cfg, fc)
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.RadarrAPIKey = "key"
	cfg.APIToken = "token"
	if err = validate(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.MountPath != "/storage/torbox" || cfg.Gateway.SubmitIntervalSeconds != 120 || !cfg.Gateway.RecoverMissing {
		t.Fatal(cfg.Gateway)
	}

	t.Setenv("GATEWAY_QB_PASSWORD", "env-secret")
	applyEnvOverrides(&cfg)
	if cfg.Gateway.QBPassword != "env-secret" {
		t.Fatal("env override missing")
	}
}

func TestOIDCConfigOverridesAndModeConflict(t *testing.T) {
	cfg := defaults()
	t.Setenv("GATEWAY_OIDC_ENABLED", "true")
	t.Setenv("GATEWAY_OIDC_ISSUER_URL", "https://auth.example/application/o/rehydrator/")
	t.Setenv("GATEWAY_OIDC_CLIENT_ID", "client")
	t.Setenv("GATEWAY_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("GATEWAY_OIDC_REDIRECT_URL", "https://gateway.example/auth/callback")
	applyEnvOverrides(&cfg)
	if !cfg.Gateway.OIDC.Enabled || cfg.Gateway.OIDC.ClientSecret != "secret" || cfg.Gateway.OIDC.Validate() != nil {
		t.Fatal("OIDC environment overrides not applied")
	}
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.Gateway.DashboardExternalAuth = true
	if err := validate(cfg); err == nil {
		t.Fatal("conflicting authentication modes accepted")
	}
}

func TestGatewayActivePollingConfig(t *testing.T) {
	cfg := defaults()
	if cfg.Gateway.ActivePollIntervalSeconds != 30 {
		t.Fatal("wrong default")
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("gateway:\n  active_poll_interval_seconds: 45\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	applyFileConfig(&cfg, fc)
	if cfg.Gateway.ActivePollIntervalSeconds != 45 {
		t.Fatal("YAML ignored")
	}
	t.Setenv("GATEWAY_ACTIVE_POLL_INTERVAL_SECONDS", "20")
	applyEnvOverrides(&cfg)
	if cfg.Gateway.ActivePollIntervalSeconds != 20 {
		t.Fatal("environment ignored")
	}
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.Gateway.Enabled = true
	for _, value := range []int{0, 14, 3601} {
		cfg.Gateway.ActivePollIntervalSeconds = value
		if err = validate(cfg); err == nil || !strings.Contains(err.Error(), "active_poll_interval_seconds") {
			t.Fatal(value, err)
		}
	}
}
