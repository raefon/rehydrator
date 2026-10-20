package db

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/model"
)

const nzbData = `<nzb><file><segments><segment bytes="1" number="1">article@example</segment></segments></file></nzb>`

func TestUsenetSourceLifecycle(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("usenet-%d", time.Now().UnixNano())
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_state WHERE tenant=$1`, tenant)
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_grab_sources WHERE tenant=$1`, tenant)
	defer repo.pool.Exec(ctx, `DELETE FROM media_cache_usenet_jobs WHERE tenant=$1`, tenant)
	g := model.GrabSource{Tenant: tenant, Arr: "sonarr", OwnerID: 55, DownloadID: "Original-Job", Torrent: model.TorrentMetadata{Kind: model.SourceNZBFile, Protocol: model.ProtocolUsenet, NZBFile: []byte(nzbData), NZBFilename: "pack.nzb", DownloadID: "Original-Job"}}
	if _, err := repo.QueueGrabSource(ctx, g); err != nil {
		t.Fatal(err)
	}
	jobs, err := repo.PendingGrabSources(ctx, tenant)
	if err != nil || len(jobs) != 1 || jobs[0].Torrent.Protocol != model.ProtocolUsenet {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	var ids []string
	for i := 0; i < 2; i++ {
		path := fmt.Sprintf("/show/episode-%d.mkv", i)
		if err := repo.RecordGrabImport(ctx, g, path); err != nil {
			t.Fatal(err)
		}
		item, err := repo.UpsertImportedEpisode(ctx, tenant, 55, i+1, 100+i, "Show", "Episode", 1, i+1, path, "sonarr", time.Now().Add(-time.Hour), 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	if err := repo.CompleteGrabSource(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		s, found, err := repo.SavedTorrentSource(ctx, id)
		if err != nil || !found || s.Protocol != model.ProtocolUsenet || s.InfoHash != "" || s.DownloadID != "Original-Job" || string(s.NZBFile) != nzbData {
			t.Fatalf("saved=%+v found=%v err=%v", s, found, err)
		}
	}
	// Concurrent episode requests must share the same runtime job mapping.
	var mu sync.Mutex
	submissions := 0
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			job, err := repo.EnsureNZBJob(ctx, id, func(current string) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				if current == "Original-Job" {
					submissions++
				}
				return "replacement-job", nil
			})
			if err != nil || job != "replacement-job" {
				t.Errorf("job=%s err=%v", job, err)
			}
		}(id)
	}
	wg.Wait()
	if submissions != 1 {
		t.Fatalf("duplicate pack submissions: %d", submissions)
	}
	for i, id := range ids {
		item, found, err := repo.GetState(ctx, tenant, model.MediaSeries, 100+i)
		if err != nil || !found || item.DownloadJobID != "replacement-job" || item.SourceProtocol != model.ProtocolUsenet || item.InfoHash != nil {
			t.Fatalf("item=%+v err=%v", item, err)
		}
		if err := repo.MarkAvailable(ctx, id, "replacement-job", time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	// Hash guardrails and automatic provider pruning must not select NZB rows.
	if rows, err := repo.PruneWorkItems(ctx, 100); err != nil {
		t.Fatal(err)
	} else {
		for _, row := range rows {
			if row.Tenant == tenant {
				t.Fatal("Usenet selected for pruning")
			}
		}
	}
	// Repeated initialization must preserve NZB payloads and constraints.
	if err := repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	s, _, err := repo.SavedTorrentSource(ctx, ids[0])
	if err != nil || s.DownloadID != "Original-Job" {
		t.Fatal("runtime job overwrote original association", err)
	}
	// Failed submissions don't erase the last known job mapping.
	if _, err := repo.EnsureNZBJob(ctx, ids[0], func(string) (string, error) { return "", fmt.Errorf("network failed") }); err == nil {
		t.Fatal("callback failure hidden")
	}
	item, _, err := repo.GetState(ctx, tenant, model.MediaSeries, 100)
	if err != nil || item.DownloadJobID != "replacement-job" {
		t.Fatal("job checkpoint lost", err)
	}
	// A later episode import must use the current pack job, not the old Arr ID.
	if err := repo.RecordGrabImport(ctx, g, "/show/episode-2.mkv"); err != nil {
		t.Fatal(err)
	}
	third, err := repo.UpsertImportedEpisode(ctx, tenant, 55, 3, 102, "Show", "Episode", 1, 3, "/show/episode-2.mkv", "sonarr", time.Now().Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	current, _, err := repo.GetState(ctx, tenant, model.MediaSeries, 102)
	if err != nil || current.DownloadJobID != "replacement-job" {
		t.Fatalf("late pack import lost current job: %+v %v", current, err)
	}
	_ = third
	// An upgrade to a torrent clears the old NZB payload and runtime job identity.
	newer := model.GrabSource{Tenant: tenant, Arr: "sonarr", OwnerID: 55, DownloadID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Torrent: model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if _, err := repo.QueueGrabSource(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteGrabSource(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordGrabImport(ctx, newer, "/show/episode-0.mkv"); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkGrabSources(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	upgraded, _, err := repo.GetState(ctx, tenant, model.MediaSeries, 100)
	if err != nil || upgraded.SourceProtocol != model.ProtocolTorrent || upgraded.DownloadJobID != "" {
		t.Fatalf("upgrade retained NZB identity: %+v %v", upgraded, err)
	}
	upgradedSource, _, err := repo.SavedTorrentSource(ctx, ids[0])
	if err != nil || upgradedSource.Kind != model.SourceMagnet || len(upgradedSource.NZBFile) != 0 {
		t.Fatal("upgrade retained NZB payload", err)
	}

}
