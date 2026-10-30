package controller

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/db"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArrMissingRequiresOptInAndHealthyCanary(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("arr-missing-%d", time.Now().UnixNano())
	dir := t.TempDir()
	link := filepath.Join(dir, "episode.mkv")
	canary := filepath.Join(dir, "canary.mkv")
	if err = os.Symlink(filepath.Join(dir, "absent.mkv"), link); err != nil {
		t.Fatal(err)
	}
	item, err := repo.UpsertImportedEpisode(ctx, tenant, 20, 30, 40, "Show", "Episode", 1, 1, link, "sonarr", time.Now(), 200)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.FlagMissingInventorySource(ctx, tenant, item.ID, link); err != nil {
		t.Fatal(err)
	}
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes++
			w.WriteHeader(200)
			return
		}
		if deletes > 0 {
			w.WriteHeader(404)
			return
		}
		fmt.Fprintf(w, `{"id":40,"seriesId":20,"path":%q}`, link)
	}))
	defer srv.Close()
	c := New(Options{Repo: repo, Tenant: tenant, Sonarr: arr.NewClient("sonarr", srv.URL, "key"), CSI: csi.NewChecker(dir),
		FileHealth:     config.FileHealthConfig{CanaryPath: canary},
		JellyfinConfig: config.JellyfinConfig{InventorySync: config.JellyfinInventoryConfig{MaxRefreshesPerRun: 5}}})
	c.reconcileArrMissing(ctx)
	if deletes != 0 {
		t.Fatal("opt-out changed Arr")
	}
	c.opt.JellyfinConfig.InventorySync.MarkMissingInArr = true
	c.reconcileArrMissing(ctx)
	if deletes != 0 {
		t.Fatal("unreadable canary allowed removal")
	}
	if err = os.WriteFile(canary, []byte("healthy"), 0600); err != nil {
		t.Fatal(err)
	}
	c.reconcileArrMissing(ctx)
	if deletes != 1 {
		t.Fatal("confirmed missing file not reconciled")
	}
	c.reconcileArrMissing(ctx)
	if deletes != 1 {
		t.Fatal("successful notification repeated")
	}
}
