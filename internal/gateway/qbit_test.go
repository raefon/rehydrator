package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&tr=udp%3A%2F%2Ftracker.example%3A80"

type fakeTorrent struct {
	*fakeProvider
	source model.TorrentMetadata
}

func (p *fakeTorrent) Submit(ctx context.Context, s model.TorrentMetadata) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.source = s
	p.creates++
	p.exists = true
	return int64(100 + p.creates), nil
}
func TestDirectGatewayArrLifecycle(t *testing.T) {
	s, provider, conn := testService(t)
	s.direct = true
	s.qbUser = "arr"
	s.qbPassword = "secret"
	torrent := &fakeTorrent{fakeProvider: provider}
	s.torrent = torrent
	ctx := context.Background()
	call := func(method, endpoint string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v2/"+endpoint, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.QBittorrent(w, r)
		return w
	}
	if w := call("GET", "app/preferences", nil, nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	login := call("POST", "auth/login", url.Values{"username": {"arr"}, "password": {"secret"}}, nil)
	if login.Code != 200 || login.Body.String() != "Ok." {
		t.Fatal(login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if w := call("GET", "app/preferences", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	add := url.Values{"category": {"sonarr"}, "urls": {testMagnet}, "paused": {"false"}, "contentLayout": {"Original"}}
	for i := 0; i < 2; i++ {
		w := call("POST", "torrents/add", add, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	jobs, err := s.repo.CloudNZBs(ctx, s.tenant)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	j := jobs[0]
	if j.InfoHash != "0123456789012345678901234567890123456789" || j.SourceKind != "magnet" || j.SourceID == "" {
		t.Fatal(j)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.creates != 1 || torrent.source.Magnet != testMagnet {
		t.Fatal("source changed or duplicate submit")
	}
	due := func() {
		t.Helper()
		_, e := conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE id=$1", j.ID)
		if e != nil {
			t.Fatal(e)
		}
	}
	provider.ready = true
	mounted := filepath.Join(s.cfg.MountPath, "torrents", j.ID, "Release", "Movie & test.mkv")
	if err = os.MkdirAll(filepath.Dir(mounted), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(mounted, []byte("abcdef"), 0644); err != nil {
		t.Fatal(err)
	}
	due()
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	info := call("GET", "torrents/info", nil, cookie)
	var listed []map[string]any
	if json.Unmarshal(info.Body.Bytes(), &listed) != nil || len(listed) != 1 || listed[0]["progress"] != float64(1) {
		t.Fatal(info.Body.String())
	}
	source, err := s.SourceForImport(ctx, model.MediaCacheState{SymlinkPath: filepath.Join(s.storage(j), "Release", "Movie & test.mkv")})
	if err != nil || source.Magnet != testMagnet {
		t.Fatal(source, err)
	}
	// Completion cleanup removes staging links but never source or DAV catalog.
	del := call("POST", "torrents/delete", url.Values{"hashes": {j.InfoHash}, "deleteFiles": {"true"}}, cookie)
	if del.Code != 200 {
		t.Fatal(del.Code, del.Body.String())
	}
	if _, err = s.Source(ctx, j.InfoHash); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("HEAD", "/webdav/torrents/"+j.ID+"/Release/Movie%20%26%20test.mkv", nil)
	req.SetBasicAuth("dav", "dav-key")
	w := httptest.NewRecorder()
	s.DAV(w, req)
	if w.Code != 200 || provider.links != 0 {
		t.Fatal(w.Code, provider.links)
	}
	// Recreate the service as after restart; sessions and original sources survive.
	restarted := New(s.cfg, s.tenant, s.repo, provider, "sonarr", "radarr")
	restarted.direct = true
	restarted.torrent = torrent
	restarted.qbUser = s.qbUser
	restarted.qbPassword = s.qbPassword
	s = restarted
	if w = call("GET", "app/version", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	provider.exists = false
	for i := 0; i < 3; i++ {
		due()
		if err = s.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = conn.Exec(ctx, "UPDATE media_cache_cloud_usenet_gate SET next_submit_at=now() WHERE tenant=$1", s.tenant)
	due()
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.creates != 2 {
		t.Fatal("one source recovery expected", provider.creates)
	}
	due()
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.repo.GatewayJob(ctx, s.tenant, j.InfoHash)
	if err != nil || recovered.State != "Completed" || recovered.ID != j.ID || recovered.ProviderID != 102 {
		t.Fatal(recovered, err)
	}
	// Authentication revocation is durable and mutation requires POST.
	if w = call("GET", "gateway/reconcile", url.Values{"id": {j.ID}}, cookie); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w = call("POST", "auth/logout", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = call("GET", "torrents/info", nil, cookie); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestTorrentFileSourceRetainedAndV2Rejected(t *testing.T) {
	s, _, _ := testService(t)
	ctx := context.Background()
	data := []byte("d4:infod6:lengthi6e4:name9:Movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789ee")
	j, err := s.repo.AddGatewaySource(ctx, s.tenant, "radarr", model.TorrentMetadata{Kind: model.SourceTorrentFile, TorrentFile: data, TorrentFilename: "private.torrent"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.Source(ctx, j.ID)
	if err != nil || source.Magnet != "" || !bytes.Equal(source.TorrentFile, data) {
		t.Fatal(source, err)
	}
	if _, err = s.repo.AddGatewaySource(ctx, s.tenant, "radarr", model.TorrentMetadata{Kind: model.SourceTorrentFile, TorrentFile: []byte("d4:infod12:meta versioni2eee")}); err == nil {
		t.Fatal("v2 accepted")
	}
	// Multipart form is the shape sent by the Arr qBittorrent clients.
	s.qbUser = "arr"
	s.qbPassword = "secret"
	login := httptest.NewRequest("POST", "/api/v2/auth/login", strings.NewReader("username=arr&password=secret"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.QBittorrent(w, login)
	cookie := w.Result().Cookies()[0]
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("category", "radarr")
	file, _ := writer.CreateFormFile("torrents", "private.torrent")
	_, _ = file.Write(data)
	_ = writer.Close()
	r := httptest.NewRequest("POST", "/api/v2/torrents/add", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.QBittorrent(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	jobs, err := s.repo.CloudNZBs(ctx, s.tenant)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	// Unknown submission is never reset by another client add/reconcile request.
	jobs[0].State = "SubmissionUnknown"
	if err = s.repo.UpdateCloudNZB(ctx, s.tenant, jobs[0], time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ensure(ctx, source, "radarr"); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.State != "SubmissionUnknown" {
		t.Fatal(current, err)
	}
}

func TestUnifiedDAVRoutesOverlappingProviderIDs(t *testing.T) {
	s, usenet, _ := testService(t)
	s.direct = true
	torrentProvider := &fakeProvider{}
	s.torrent = &fakeTorrent{fakeProvider: torrentProvider}
	ctx := context.Background()
	torrent, err := s.repo.AddGatewaySource(ctx, s.tenant, "sonarr", model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: testMagnet})
	if err != nil {
		t.Fatal(err)
	}
	nzb, err := s.repo.AddCloudNZB(ctx, s.tenant, "radarr", "original.nzb", []byte(testNZB))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*fakeProvider{usenet, torrentProvider} {
		provider := p
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("gateway credentials leaked upstream")
			}
			if r.Header.Get("Range") != "bytes=1-2" {
				t.Error("range missing")
			}
			w.Header().Set("Content-Range", "bytes 1-2/6")
			w.WriteHeader(206)
			if provider == usenet {
				_, _ = w.Write([]byte("NZ"))
			} else {
				_, _ = w.Write([]byte("BT"))
			}
		}))
		defer server.Close()
		p.link = server.URL
	}
	for _, j := range []db.CloudNZBJob{torrent, nzb} {
		j.ProviderID = 42
		j.State = "Completed"
		j.Files = []torbox.UsenetFile{{ID: 0, Name: "Movie.mkv", Size: 6}}
		if err = s.repo.UpdateCloudNZB(ctx, s.tenant, j, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	for _, j := range []db.CloudNZBJob{torrent, nzb} {
		for i := 0; i < 2; i++ {
			r := httptest.NewRequest("GET", "/webdav/"+s.catalogPath(j)+"/Movie.mkv", nil)
			r.SetBasicAuth("dav", "dav-key")
			r.Header.Set("Range", "bytes=1-2")
			w := httptest.NewRecorder()
			s.DAV(w, r)
			want := "NZ"
			if j.Protocol == model.ProtocolTorrent {
				want = "BT"
			}
			if w.Code != 206 || w.Body.String() != want {
				t.Fatal(w.Code, w.Body.String(), want)
			}
		}
	}
	if usenet.links != 1 || torrentProvider.links != 1 {
		t.Fatal("protocol cache collision or repeated API calls", usenet.links, torrentProvider.links)
	}
}
