package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectGatewayConfiguration(t *testing.T) {
	cfg := defaults()
	file := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(file, []byte("gateway:\n  enabled: true\n  qb_username: arr\n  qb_password: secret\n  api_key: torbox\n  sab_api_key: sab\n  dav_username: dav\n  dav_password: secret\nprune_enabled: false\n"), 0600)
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
	cfg.DecypharrURL = ""
	if err = validate(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.MountPath != "/storage/torbox" || cfg.Gateway.SubmitIntervalSeconds != 120 || !cfg.Gateway.RecoverMissing {
		t.Fatal(cfg.Gateway)
	}
	cfg.CloudUsenet.Enabled = true
	if validate(cfg) == nil {
		t.Fatal("two workers allowed")
	}
	cfg.CloudUsenet.Enabled = false
	cfg.PruneEnabled = true
	if validate(cfg) == nil {
		t.Fatal("unimplemented provider pruning allowed")
	}
	t.Setenv("GATEWAY_QB_PASSWORD", "env-secret")
	applyEnvOverrides(&cfg)
	if cfg.Gateway.QBPassword != "env-secret" {
		t.Fatal("env override missing")
	}
}
