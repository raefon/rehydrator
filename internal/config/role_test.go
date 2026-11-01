package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitRoleConfiguration(t *testing.T) {
	cfg := defaults()
	cfg.Role = RoleGateway
	cfg.Gateway.Enabled = true
	cfg.PruneEnabled = false
	cfg.PostgresURL = "postgres://example"
	cfg.Tenant = "tenant"
	cfg.Gateway.APIKey = "provider"
	cfg.Gateway.QBUsername = "arr"
	cfg.Gateway.QBPassword = "secret"
	cfg.Gateway.SABAPIKey = "sab"
	cfg.Gateway.DAVUsername = "dav"
	cfg.Gateway.DAVPassword = "secret"
	// No Arr, API admin token, CSI path or controller integration is required.
	cfg.RadarrAPIKey = ""
	cfg.CSIPath = ""
	cfg.APIToken = ""
	if err := validate(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Role = RoleController
	cfg.RadarrAPIKey = "arr"
	cfg.CSIPath = "/storage/media"
	cfg.APIToken = "admin"
	cfg.TorBoxHealth.Enabled = true // Legacy provider health is not run by the direct controller.
	cfg.TorBoxHealth.APIKey = ""
	cfg.Gateway.APIKey = ""
	cfg.Gateway.QBPassword = ""
	cfg.Gateway.SABAPIKey = ""
	cfg.Gateway.DAVPassword = ""
	if err := validate(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Gateway.Enabled = false
	if validate(cfg) == nil {
		t.Fatal("split role without direct gateway accepted")
	}
	cfg.Role = "bad"
	if validate(cfg) == nil {
		t.Fatal("invalid role accepted")
	}
}
func TestCommandLineRoleOverridesFileAndEnvironment(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(file, []byte("role: controller\npostgres_url: postgres://example\ntenant: tenant\nprune_enabled: false\ngateway:\n  enabled: true\n  api_key: provider\n  qb_username: arr\n  qb_password: secret\n  sab_api_key: sab\n  dav_username: dav\n  dav_password: secret\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REHYDRATOR_ROLE", "combined")
	cfg, err := LoadWithRole(file, RoleGateway)
	if err != nil || cfg.Role != RoleGateway {
		t.Fatal(cfg.Role, err)
	}
}
