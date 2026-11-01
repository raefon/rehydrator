package main

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStandaloneGatewayStartsWithoutTenantFilesystemOrIntegrations(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	cfg := config.Config{Role: config.RoleGateway, Tenant: fmt.Sprintf("gateway-main-%d", time.Now().UnixNano()), HealthAddr: addr, Gateway: config.DefaultGateway(), DecypharrSonarrCategory: "sonarr", DecypharrRadarrCategory: "radarr"}
	cfg.Gateway.APIKey = "unused-no-jobs"
	cfg.Gateway.DAVUsername = "dav"
	cfg.Gateway.DAVPassword = "secret"
	cfg.Gateway.MountPath = filepath.Join(t.TempDir(), "missing-mount")
	cfg.Gateway.DownloadPath = filepath.Join(t.TempDir(), "missing-staging")
	done := make(chan struct{})
	go func() { defer close(done); runGateway(ctx, cfg, repo) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("gateway did not stop")
		}
	})
	client := &http.Client{Timeout: time.Second}
	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, e := client.Get(base + "/readyz")
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway never ready", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	req, _ := http.NewRequest("PROPFIND", base+"/webdav/", nil)
	req.SetBasicAuth("dav", "secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 207 {
		t.Fatal(resp.StatusCode)
	}
	resp, err = client.Get(base + "/api/state/summary")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("gateway exposed controller API", resp.StatusCode)
	}
	for _, path := range []string{cfg.Gateway.MountPath, cfg.Gateway.DownloadPath} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("gateway touched tenant path", path, err)
		}
	}
}
