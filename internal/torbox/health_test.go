package torbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const healthHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestHealthInventoryClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       Availability
		reason     string
		httpStatus int
		fail       bool
	}{
		{"present", `{"success":true,"data":[{"id":1,"hash":"` + healthHash + `","download_present":true}]}`, Available, "download_present", 200, false},
		{"evicted", `{"success":true,"data":[{"id":1,"hash":"` + healthHash + `","download_present":false}]}`, Unavailable, "download_not_present", 200, false},
		{"absent", `{"success":true,"data":[]}`, Unavailable, "not_in_account", 200, false},
		{"missing flag", `{"success":true,"data":[{"id":1,"hash":"` + healthHash + `"}]}`, Unknown, "incomplete_or_active_download", 200, false},
		{"active", `{"success":true,"data":[{"id":1,"hash":"` + healthHash + `","download_present":false,"download_state":"downloading"}]}`, Unknown, "incomplete_or_active_download", 200, false},
		{"rate limit", `{}`, Unknown, "", 429, true},
		{"unauthorized", `{}`, Unknown, "", 401, true},
		{"missing data", `{"success":true}`, Unknown, "", 200, true},
		{"failed response", `{"success":false,"data":[]}`, Unknown, "", 200, true},
		{"bad hash", `{"success":true,"data":[{"id":1,"download_present":false}]}`, Unknown, "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/torrents/mylist" || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("bypass_cache") != "true" {
					t.Error("not a read-only authenticated account check")
				}
				w.WriteHeader(tc.httpStatus)
				if calls == 1 {
					_, _ = fmt.Fprint(w, tc.body)
				} else {
					_, _ = fmt.Fprint(w, `{"success":true,"data":[]}`)
				}
			}))
			defer server.Close()
			c := NewHealthClient("secret")
			c.base = server.URL
			states, err := c.Availability(context.Background(), []string{healthHash, healthHash})
			if tc.fail {
				if err == nil || states != nil {
					t.Fatal("API failure became eviction", states, err)
				}
				return
			}
			if err != nil || states[healthHash].Status != tc.want || states[healthHash].Reason != tc.reason {
				t.Fatalf("states=%+v err=%v", states, err)
			}
		})
	}
}

func TestHealthInventoryPaginationFailureDoesNotInferAbsence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "0" {
			_, _ = fmt.Fprint(w, `{"success":true,"data":[{"id":1,"hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","download_present":true}]}`)
		} else {
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	c := NewHealthClient("secret")
	c.base = server.URL
	if result, err := c.Availability(context.Background(), []string{healthHash}); err == nil || result != nil {
		t.Fatal("partial inventory treated as complete")
	}
}
