package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/jellyfin"
	"github.com/raefon/rehydrator/internal/model"
)

func TestJellyfinImportNotificationAndDedupe(t *testing.T) {
	paths := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/Library/Media/Updated" {
			t.Error("wrong refresh API")
		}
		var body struct{ Updates []struct{ Path string } }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(body.Updates) != 1 {
			t.Error("expected one targeted path")
			return
		}
		paths <- body.Updates[0].Path
		w.WriteHeader(204)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New(Options{Jellyfin: jellyfin.NewClient(server.URL, "secret", time.Second), JellyfinConfig: config.JellyfinConfig{Enabled: true, RefreshAfterImport: true, TimeoutSeconds: 1, MaxRefreshesPerRun: 1, RefreshDedupeMinutes: 10}})
	item := model.MediaCacheState{Tenant: "test", MediaType: model.MediaMovie, ArrID: 7, SymlinkPath: "/media/movie.mkv"}
	c.ScheduleInitialImportJellyfinRefresh(ctx, item, "initial_import")
	select {
	case path := <-paths:
		if path != item.SymlinkPath {
			t.Fatal("notified wrong path")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("import refresh never ran")
	}
	if reserved, _ := c.reserveJellyfinRefresh(ctx, item, "self_heal_available", item.SymlinkPath); reserved {
		t.Fatal("duplicate automatic refresh not suppressed")
	}
}
