package db

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudUploadReservationSurvivesDuplicateRequests(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("cloud-reservation-%d", time.Now().UnixNano())
	defer r.pool.Exec(ctx, "DELETE FROM gateway_jobs WHERE tenant=$1", tenant)
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_cloud_usenet_gate WHERE tenant=$1", tenant)
	j, err := r.AddGatewayNZB(ctx, tenant, "sonarr", "source.nzb", []byte(nzbData))
	if err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, found, err := r.ClaimGatewayJob(ctx, tenant, 2*time.Minute)
			if err != nil {
				t.Error(err)
			}
			if found {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatal("duplicate upload reservations", claims.Load())
	}
	if _, err = r.AddGatewayNZB(ctx, tenant, "sonarr", "source.nzb", []byte(nzbData)); err != nil {
		t.Fatal(err)
	}
	// The duplicate must not expire an in-flight submission lease.
	if _, found, err := r.ClaimGatewayJob(ctx, tenant, 2*time.Minute); err != nil || found {
		t.Fatal("duplicate expired submission lease", found, err)
	}
	current, err := r.GatewayJobByID(ctx, tenant, j.ID)
	if err != nil || current.State != "Submitting" || current.Attempts != 1 {
		t.Fatal(current, err)
	}
	// A separate job cannot bypass the shared account submission gap.
	if _, err = r.AddGatewayNZB(ctx, tenant, "radarr", "other.nzb", []byte(nzbData)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := r.ClaimGatewayJob(ctx, tenant, 2*time.Minute); err != nil || found {
		t.Fatal("upload gate bypassed", found, err)
	}
}

func TestGatewaySourceSurvivesMediaCleanupAndCloudMigration(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("gateway-retention-%d", time.Now().UnixNano())
	source := model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	j, err := r.AddGatewaySource(ctx, tenant, "radarr", source)
	if err != nil {
		t.Fatal(err)
	}
	item, err := r.UpsertImportedMovie(ctx, tenant, 77, "Movie", "/library/movie.mkv", "radarr", time.Now(), 123, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.SaveTorrentMetadata(ctx, item.ID, source, "radarr"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	retained, err := r.GatewayJob(ctx, tenant, j.ID)
	if err != nil || retained.Magnet != source.Magnet {
		t.Fatal(retained, err)
	}
	var magnet string
	if err = r.pool.QueryRow(ctx, "SELECT magnet FROM gateway_sources WHERE id=$1", j.SourceID).Scan(&magnet); err != nil || magnet != source.Magnet {
		t.Fatal(magnet, err)
	}
	var legacyID string
	err = r.pool.QueryRow(ctx, `INSERT INTO media_cache_cloud_usenet(tenant,category,fingerprint,filename,nzb,name,state,provider_id) VALUES($1,'sonarr','legacy-fingerprint','original.nzb',$2,'Legacy','Completed',123) RETURNING id`, tenant, []byte(nzbData)).Scan(&legacyID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.pool.Exec(ctx, "DELETE FROM media_cache_cloud_usenet WHERE tenant=$1", tenant)
	sql, err := os.ReadFile("../../migrations/032_direct_gateway.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = r.pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	copied, err := r.GatewayJob(ctx, tenant, legacyID)
	if err != nil || string(copied.NZB) != nzbData || copied.ProviderID != 123 || copied.State != "Completed" || copied.SourceID == "" {
		t.Fatal(copied, err)
	}
}
