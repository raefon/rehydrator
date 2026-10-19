package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

func TestGrabCaptureUsesExactDownloadAndOwner(t *testing.T) {
	const hash = "7952549975b3256302ada821a1295c5a1ca628dc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"records":[
 {"eventType":"grabbed","movieId":99,"downloadId":"%s","data":{"downloadUrl":"http://wrong-owner/file"}},
 {"eventType":"grabbed","movieId":77,"downloadId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data":{"downloadUrl":"http://old-release/file"}},
 {"eventType":"grabbed","movieId":77,"downloadId":"%s","data":{"downloadUrl":"http://correct-release/file"}}
 ]}`, hash, hash)
	}))
	defer server.Close()
	c := NewClient("radarr", server.URL, "secret")
	source, err := c.GrabbedTorrentByDownloadID(context.Background(), 77, model.MediaMovie, hash)
	if err != nil || source.TorrentURL != "http://correct-release/file" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	if _, err := c.GrabbedTorrentByDownloadID(context.Background(), 78, model.MediaMovie, hash); err == nil {
		t.Fatal("mismatched movie accepted")
	}
}

func TestHistoryPreservesSourceType(t *testing.T) {
	const hash = "7952549975b3256302ada821a1295c5a1ca628dc"
	const magnet = "magnet:?xt=urn:btih:" + hash
	for _, tc := range []struct {
		name, url, guid string
		kind            model.SourceKind
		wantMagnet      string
	}{
		{"file_url", "http://indexer/release.torrent", magnet, model.SourceUnresolved, ""},
		{"magnet", magnet, "", model.SourceMagnet, magnet},
		{"guid_magnet", "", magnet, model.SourceMagnet, magnet},
		{"hash_only", "", "", model.SourceRecoveryRequired, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, ok := torrentMetadataFromGrabbed("radarr", 77, model.MediaMovie, HistoryRecord{EventType: "grabbed", DownloadID: hash, Data: map[string]any{"downloadUrl": tc.url, "guid": tc.guid}})
			if !ok || source.Kind != tc.kind || source.Magnet != tc.wantMagnet {
				t.Fatalf("source=%+v", source)
			}
			if tc.kind == model.SourceUnresolved && source.TorrentURL != tc.url {
				t.Fatal("file URL lost")
			}
		})
	}
}
