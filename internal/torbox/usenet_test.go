package torbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudUsenetAPIContract(t *testing.T) {
	nzb := `<nzb><file><segments><segment bytes="1" number="1">a@b</segment></segments></file></nzb>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("missing authorization")
		}
		switch r.URL.Path {
		case "/usenet/createusenetdownload":
			if r.Method != "POST" {
				t.Error("not POST")
			}
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("as_queued") != "false" {
				t.Error("submission explicitly requested provider queue")
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			data, _ := io.ReadAll(f)
			if string(data) != nzb || h.Filename != "original.nzb" || r.FormValue("post_processing") != "-1" {
				t.Error("source altered")
			}
			fmt.Fprint(w, `{"success":true,"data":{"usenetdownload_id":123}}`)
		case "/usenet/mylist":
			if r.URL.Query().Get("id") != "123" {
				t.Error("wrong provider ID")
			}
			fmt.Fprint(w, `{"success":true,"data":[{"id":123,"download_state":"cached","download_present":true,"files":[{"id":0,"name":"release/movie.mkv","size":100}]}]}`)
		case "/usenet/requestdl":
			if r.URL.Query().Get("token") != "key" || r.URL.Query().Get("usenet_id") != "123" || r.URL.Query().Get("file_id") != "0" || r.URL.Query().Get("redirect") != "false" {
				t.Error("wrong link query")
			}
			fmt.Fprint(w, `{"success":true,"data":"https://cdn.example/file"}`)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := NewUsenetClient("key")
	client.base = server.URL
	id, err := client.Create(context.Background(), []byte(nzb), "original.nzb")
	if err != nil || id != 123 {
		t.Fatal(id, err)
	}
	download, found, err := client.Get(context.Background(), id)
	if err != nil || !found || !download.Ready() {
		t.Fatal(download, found, err)
	}
	link, err := client.Link(context.Background(), id, 0)
	if err != nil || link != "https://cdn.example/file" {
		t.Fatal(link, err)
	}
}
func TestCloudUsenetRejectsUnknownAndUnsafeData(t *testing.T) {
	for _, body := range []string{`{"success":true,"data":null}`, `{"success":false,"error":"RATE_LIMITED"}`, `{"success":true,"data":[{"id":999}]}`, `{"success":true,"data":[{"id":123,"files":[{"id":0,"name":"../escape","size":1}]}]}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer srv.Close()
			c := NewUsenetClient("sensitive")
			c.base = srv.URL
			_, _, err := c.Get(context.Background(), 123)
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("unsafe response accepted or key leaked", err)
			}
		})
	}
}

func TestQueuedUsenetResponseNeverBecomesActiveID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"queued_id":987,"usenetdownload_id":null}}`)
	}))
	defer srv.Close()
	c := NewUsenetClient("secret")
	c.base = srv.URL
	id, err := c.Create(context.Background(), []byte(`<nzb/>`), "original.nzb")
	if id != 0 || err == nil || !strings.Contains(err.Error(), "queued_id=987") {
		t.Fatal(id, err)
	}
}

func TestFindNZBSubmissionRequiresUniqueMarker(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       int64
		fail       bool
	}{
		{"marked", `[{"id":42,"name":"cloud-nzb-job--Release"}]`, 42, false},
		{"extension", `[{"id":42,"name":"cloud-nzb-job--Release.nzb"}]`, 42, false},
		{"legacy title", `[{"id":42,"name":"Release"}]`, 0, false},
		{"other job", `[{"id":42,"name":"cloud-nzb-other--Release"}]`, 0, false},
		{"ambiguous", `[{"id":42,"name":"cloud-nzb-job--Release"},{"id":43,"name":"cloud-nzb-job--Release"}]`, 0, true},
		{"duplicate ID", `[{"id":42,"name":"cloud-nzb-job--Release"},{"id":42,"name":"cloud-nzb-job--Release"}]`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("recovery uploaded a second NZB")
				}
				fmt.Fprintf(w, `{"success":true,"data":%s}`, tc.data)
			}))
			defer server.Close()
			c := NewUsenetClient("key")
			c.base = server.URL
			id, err := c.FindSubmission(context.Background(), "cloud-nzb-job", "Release.nzb")
			if id != tc.want || (err != nil) != tc.fail {
				t.Fatalf("got %d, %v", id, err)
			}
		})
	}
}

func TestUsenetStaleStatusCrossCheck(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		ready      bool
	}{
		{"ready same ID", `[{"id":123,"name":"cloud-nzb-job--Release","download_state":"completed","download_present":true,"files":[{"id":0,"name":"Movie.mkv","size":10}]}]`, true},
		{"other ID", `[{"id":124,"download_state":"completed","files":[{"id":0,"name":"Movie.mkv","size":10}]}]`, false},
		{"no files", `[{"id":123,"download_state":"completed","download_present":true}]`, false},
		{"absent files", `[{"id":123,"download_state":"completed","download_present":false,"files":[{"id":0,"name":"Movie.mkv","size":10}]}]`, false},
		{"unsafe catalog", `[{"id":123,"download_state":"completed","files":[{"id":0,"name":"../Movie.mkv","size":10}]}]`, false},
		{"lookup failed", `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("id") != "" {
					fmt.Fprint(w, `{"success":true,"data":[{"id":123,"download_state":"processing","download_present":false,"download_finished":true}]}`)
					return
				}
				fmt.Fprintf(w, `{"success":true,"data":%s}`, tc.data)
			}))
			defer server.Close()
			c := NewUsenetClient("key")
			c.base = server.URL
			d, found, err := c.Get(context.Background(), 123)
			if err != nil || !found || d.Ready() != tc.ready {
				t.Fatalf("got %+v, %v, %v", d, found, err)
			}
			if tc.ready && d.Name != "Release" {
				t.Fatalf("transport marker leaked into release name: %q", d.Name)
			}
		})
	}
}
