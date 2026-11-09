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

func TestPlacementLookupRequiresExplicitNotFound(t *testing.T) {
	for _, protocol := range []string{"torrent", "usenet"} {
		for _, tc := range []struct {
			name   string
			status int
			body   string
			absent bool
		}{
			{"confirmed absence", 404, `{"success":false,"error":"ITEM_NOT_FOUND"}`, true},
			{"generic 404", 404, `{"success":false,"error":"NOT_FOUND"}`, false},
			{"proxy 404", 404, `<html>not found</html>`, false},
			{"unauthorized", 401, `{"success":false,"error":"ITEM_NOT_FOUND"}`, false},
			{"forbidden", 403, `{"success":false,"error":"ITEM_NOT_FOUND"}`, false},
			{"rate limited", 429, `{"success":false,"error":"RATE_LIMITED"}`, false},
			{"server failure", 500, `{"success":false,"error":"ITEM_NOT_FOUND"}`, false},
			{"unconfirmed success", 200, `{"success":false,"error":"ITEM_NOT_FOUND"}`, false},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/"+map[string]string{"torrent": "torrents", "usenet": "usenet"}[protocol]+"/mylist" || r.URL.Query().Get("id") != "42" {
						t.Error("unexpected lookup")
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				}))
				defer server.Close()
				c := NewUsenetClient("private")
				c.base = server.URL
				var found bool
				var err error
				if protocol == "torrent" {
					_, found, err = (&TorrentClient{c}).Get(context.Background(), 42)
				} else {
					_, found, err = c.Get(context.Background(), 42)
				}
				if found || (err == nil) != tc.absent {
					t.Fatalf("found=%v err=%v; absence=%v", found, err, tc.absent)
				}
			})
		}
	}
}
