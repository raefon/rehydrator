package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInventoryPathsCompletePaginationAndVersions(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/Items" || r.URL.Query().Get("IncludeItemTypes") != "Movie,Episode" || r.Header.Get("Authorization") != `MediaBrowser Token="key"` {
			t.Errorf("unexpected request: %v", r)
		}
		if calls == 1 {
			fmt.Fprint(w, `{"Items":[{"Id":"a","Path":"/media/episode.mkv","MediaSources":[{"Path":"/media/alternate.mkv"}]}],"StartIndex":0,"TotalRecordCount":2}`)
		} else {
			if r.URL.Query().Get("StartIndex") != "1" {
				t.Error("pagination offset not advanced")
			}
			fmt.Fprint(w, `{"Items":[{"Id":"b","Path":"/movies/movie.mkv"}],"StartIndex":1,"TotalRecordCount":2}`)
		}
	}))
	defer srv.Close()
	paths, err := NewClient(srv.URL, "key", time.Second).InventoryPaths(context.Background())
	if err != nil || calls != 2 || len(paths) != 3 || !paths["/movies/movie.mkv"] || !paths["/media/alternate.mkv"] {
		t.Fatalf("paths=%v calls=%d err=%v", paths, calls, err)
	}
}
func TestInventoryRejectsIncompleteResponses(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"Items":[],"TotalRecordCount":2,"StartIndex":0}`,
		`{"Items":[{"Id":"a","Path":"/media/a"}],"TotalRecordCount":2,"StartIndex":0}`,
		`{"Items":[{"Id":"a"},{"Id":"a"}],"TotalRecordCount":2,"StartIndex":0}`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer srv.Close()
			paths, err := NewClient(srv.URL, "key", time.Second).InventoryPaths(context.Background())
			if err == nil || paths != nil {
				t.Fatal("partial inventory authorized missing inference", paths, err)
			}
		})
	}
}
func TestInventoryPathMapping(t *testing.T) {
	mappings := map[string]string{"/storage/media": "/library", "/storage/media/series": "/tv"}
	for input, want := range map[string]string{"/storage/media/series/Show/a.mkv": "/tv/Show/a.mkv", "/storage/media/movies/a.mkv": "/library/movies/a.mkv", "/storage/mediastore/a": "/storage/mediastore/a", "http://remote/file": ""} {
		if got := MapInventoryPath(input, mappings); got != want {
			t.Errorf("%s: %s != %s", input, got, want)
		}
	}
}
