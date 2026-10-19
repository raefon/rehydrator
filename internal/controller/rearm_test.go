package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raefon/rehydrator/internal/arr"
	"github.com/raefon/rehydrator/internal/decypharr"
	"github.com/raefon/rehydrator/internal/model"
)

func TestHashOnlyAndLegacyMagnetRequireRecovery(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	magnet := "magnet:?xt=urn:btih:" + hash
	for _, mediaType := range []model.MediaType{model.MediaMovie, model.MediaSeries} {
		for _, withMagnet := range []bool{false, true} {
			item := model.MediaCacheState{InfoHash: &hash, MediaType: mediaType}
			if withMagnet {
				item.Magnet = &magnet
			}
			c := New(Options{})
			if _, err := c.resolveRearmTorrent(context.Background(), item); !errors.Is(err, model.ErrSourceRecoveryRequired) {
				t.Fatalf("unproven legacy source accepted: %v", err)
			}
		}
	}
}

func TestRearmRecoversActualMagnetFromArr(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	const magnet = "magnet:?xt=urn:btih:" + hash + "&tr=udp%3A%2F%2Ftracker.example"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/history" || r.URL.Query().Get("movieId") != "77" {
			t.Error("wrong history lookup")
		}
		_, _ = fmt.Fprintf(w, `{"records":[{"id":1,"movieId":77,"eventType":"grabbed","downloadId":"%s","data":{"downloadUrl":"%s"}}]}`, hash, magnet)
	}))
	defer server.Close()
	c := New(Options{Radarr: arr.NewClient("radarr", server.URL, "secret"), Decypharr: decypharr.NewClient("http://unused", "", "")})
	torrent, err := c.resolveRearmTorrent(context.Background(), model.MediaCacheState{ArrID: 77, MediaType: model.MediaMovie})
	if err != nil {
		t.Fatal(err)
	}
	if torrent.Kind != model.SourceMagnet || torrent.Magnet != magnet || torrent.InfoHash != hash || torrent.TorrentURL != "" || len(torrent.TorrentFile) > 0 {
		t.Fatalf("wrong recovered source: %+v", torrent)
	}
	otherHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := c.resolveRearmTorrent(context.Background(), model.MediaCacheState{ArrID: 77, MediaType: model.MediaMovie, InfoHash: &otherHash}); !errors.Is(err, model.ErrSourceRecoveryRequired) {
		t.Fatal("recovery silently changed the tracked release", err)
	}

}
