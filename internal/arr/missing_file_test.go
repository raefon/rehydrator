package arr

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndividualMissingFileRemoval(t *testing.T) {
	for _, kind := range []string{"sonarr", "radarr"} {
		t.Run(kind, func(t *testing.T) {
			deletes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "key" {
					t.Error("missing auth")
				}
				if r.Method == "GET" {
					if deletes > 0 {
						if kind == "sonarr" {
							w.WriteHeader(404)
						} else {
							fmt.Fprint(w, `{"id":10,"movieFile":null}`)
						}
						return
					}
					if kind == "sonarr" {
						fmt.Fprint(w, `{"id":10,"seriesId":20,"path":"/media/missing.mkv"}`)
					} else {
						fmt.Fprint(w, `{"id":10,"movieFile":{"id":99,"path":"/media/missing.mkv"}}`)
					}
				} else {
					expected := "/api/v3/moviefile/99"
					if kind == "sonarr" {
						expected = "/api/v3/episodefile/10"
					}
					if r.Method != "DELETE" || r.URL.Path != expected {
						t.Errorf("wrong deletion %s %s", r.Method, r.URL.Path)
					}
					deletes++
					w.WriteHeader(200)
				}
			}))
			defer srv.Close()
			parent := 20
			item := model.MediaCacheState{ArrID: 10, MediaType: model.MediaMovie, SymlinkPath: "/media/missing.mkv", SeriesID: &parent}
			if kind == "sonarr" {
				item.MediaType = model.MediaSeries
			}
			client := NewClient(kind, srv.URL, "key")
			if err := client.RemoveMissingImportedFile(context.Background(), item, func(context.Context, string) (bool, error) { return false, nil }); err == nil || deletes != 0 {
				t.Fatal("restored file deletion allowed", err)
			}
			wrong := item
			wrong.SymlinkPath = "/media/replacement.mkv"
			if err := client.RemoveMissingImportedFile(context.Background(), wrong, func(context.Context, string) (bool, error) { return true, nil }); err == nil || deletes != 0 {
				t.Fatal("changed path deletion allowed", err)
			}
			if err := client.RemoveMissingImportedFile(context.Background(), item, func(context.Context, string) (bool, error) { return true, nil }); err != nil || deletes != 1 {
				t.Fatal("confirmed missing file not removed", err)
			}
		})
	}
}
