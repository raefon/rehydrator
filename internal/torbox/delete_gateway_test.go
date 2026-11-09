package torbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeletePlacementContracts(t *testing.T) {
	for _, protocol := range []string{"torrent", "usenet"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				endpoint, key := "/usenet/controlusenetdownload", "usenet_id"
				if protocol == "torrent" {
					endpoint, key = "/torrents/controltorrent", "torrent_id"
				}
				if r.Method != "POST" || r.URL.Path != endpoint || r.Header.Get("Authorization") != "Bearer private" {
					t.Errorf("invalid request: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Fatal("invalid body")
				}
				if body[key] != float64(42) || body["all"] != false || body["operation"] != "delete" {
					t.Error(body)
				}
				_, _ = w.Write([]byte(`{"success":true,"data":null}`))
			}))
			defer server.Close()
			c := NewUsenetClient("private")
			c.base = server.URL
			var err error
			if protocol == "torrent" {
				err = (&TorrentClient{c}).Delete(context.Background(), 42)
			} else {
				err = c.Delete(context.Background(), 42)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDeleteRequiresConfirmedProviderSuccess(t *testing.T) {
	for _, body := range []string{`{"success":false,"detail":"PRIVATE"}`, `bad PRIVATE`, `{}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		c := NewUsenetClient("private")
		c.base = server.URL
		err := c.Delete(context.Background(), 42)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}
