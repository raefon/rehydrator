package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloudUsenetConfigIsOptInAndRequiresDeliveryCredentials(t *testing.T) {
	cfg := defaults()
	if cfg.CloudUsenet.Enabled || !cfg.CloudUsenet.RecoverMissing || cfg.CloudUsenet.SubmitIntervalSeconds < 60 {
		t.Fatal("unsafe cloud defaults")
	}
	file := filepath.Join(t.TempDir(), "config.yaml")

	if err := os.WriteFile(file, []byte("cloud_usenet:\n  enabled: true\n  mount_path: /cloud\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fc, err := readFileConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	applyFileConfig(&cfg, fc)
	if !cfg.CloudUsenet.Enabled || cfg.CloudUsenet.MountPath != "/cloud" || cfg.CloudUsenet.SubmitIntervalSeconds != 120 {
		t.Fatal(cfg.CloudUsenet)
	}
	t.Setenv("CLOUD_USENET_API_KEY", "torbox")
	t.Setenv("CLOUD_USENET_SAB_API_KEY", "sab")
	t.Setenv("CLOUD_USENET_DAV_USERNAME", "dav")
	t.Setenv("CLOUD_USENET_DAV_PASSWORD", "secret")
	applyEnvOverrides(&cfg)
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "test"
	cfg.RadarrAPIKey = "key"
	cfg.APIToken = "token"
	if err = validate(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.CloudUsenet.SubmitIntervalSeconds = 10
	if validate(cfg) == nil {
		t.Fatal("unsafe upload rate accepted")
	}
	cfg.CloudUsenet.SubmitIntervalSeconds = 120
	cfg.CloudUsenet.DownloadPath = "/cloud/downloads"
	if validate(cfg) == nil {
		t.Fatal("recursive mount/download paths accepted")
	}
}
