package config

import (
	"strings"
	"testing"
)

func TestLibraryEditionIdentity(t *testing.T) {
	cfg := Config{Tenant: "tenet-alice"}
	if cfg.StateTenant() != "tenet-alice" {
		t.Fatal("standard identity changed")
	}
	cfg.LibraryEdition = "4k"
	if cfg.StateTenant() != "tenet-alice:4k" || cfg.Tenant != "tenet-alice" {
		t.Fatal("edition isolation failed")
	}
	g := DefaultGateway()
	g.AdditionalCategories = []string{"sonarr-4k", "radarr-4k"}
	if len(g.Categories()) != 4 {
		t.Fatal(g.Categories())
	}
	cfg.LibraryPaths = []string{"/storage/media/movies-4k"}
	cfg.Role = RoleGateway
	if validate(cfg) == nil {
		t.Fatal("gateway accepted an edition scope")
	}
	cfg.LibraryEdition = "invalid"
	if validate(cfg) == nil {
		t.Fatal("unsupported edition accepted")
	}
}

func TestFourKExampleAndAdditionalCategoryValidation(t *testing.T) {
	fc, err := readFileConfig("../../config/rehydrator.controller-4k.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if fc.LibraryEdition != "4k" || len(fc.LibraryPaths) != 2 || len(fc.Gateway.Categories()) != 4 {
		t.Fatal("invalid 4k example")
	}
	cfg := defaults()
	cfg.Role = RoleController
	cfg.Gateway.AdditionalCategories = []string{"../escape"}
	if err = validate(cfg); err == nil || !strings.Contains(err.Error(), "categories") {
		t.Fatal("unsafe additional category accepted", err)
	}
}
