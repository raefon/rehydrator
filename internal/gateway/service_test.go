package gateway

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
)

const testNZB = `<nzb><file><segments><segment bytes="1" number="1">article@example</segment></segments></file></nzb>`

type fakeProvider struct {
	mu                 sync.Mutex
	creates, links     int
	submissionFilename string
	exists, ready      bool
	link               string
}

func (p *fakeProvider) Create(ctx context.Context, nzb []byte, name string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creates++
	p.submissionFilename = name
	p.exists = true
	if string(nzb) != testNZB || !strings.HasSuffix(name, "--original.nzb") {
		return 0, fmt.Errorf("source changed")
	}
	return int64(100 + p.creates), nil
}
func (p *fakeProvider) Get(ctx context.Context, id int64) (torbox.UsenetDownload, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := "downloading"
	if p.ready {
		state = "cached"
	}
	return torbox.UsenetDownload{ID: id, State: state, Files: []torbox.UsenetFile{{ID: 1, Name: "Release/empty.nfo", Size: 0}, {ID: 0, Name: "Release/Movie & test.mkv", Size: 6}}}, p.exists, nil
}
func (p *fakeProvider) Link(ctx context.Context, id, file int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.links++
	return p.link, nil
}
func testService(t *testing.T) (*Service, *fakeProvider, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	cfg := config.DefaultGatewayStorage()
	cfg.MountPath = filepath.Join(t.TempDir(), "mount")
	cfg.DownloadPath = filepath.Join(t.TempDir(), "downloads")
	cfg.SABAPIKey = "sab-key"
	cfg.DAVUsername = "dav"
	cfg.DAVPassword = "dav-key"
	provider := &fakeProvider{}
	tenant := strings.ReplaceAll(t.Name(), "/", "-")
	s := newService(cfg, tenant, repo, provider, "sonarr", "radarr")
	t.Cleanup(func() {
		conn.Exec(ctx, "DELETE FROM gateway_jobs WHERE tenant=$1", tenant)
		conn.Exec(ctx, "DELETE FROM media_cache_cloud_usenet_gate WHERE tenant=$1", tenant)
	})
	return s, provider, conn
}
func addTestJob(t *testing.T, s *Service) db.GatewayJob {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("mode", "addfile")
	_ = w.WriteField("cat", "radarr")
	_ = w.WriteField("apikey", "sab-key")
	f, _ := w.CreateFormFile("name", "original.nzb")
	_, _ = io.WriteString(f, testNZB)
	w.Close()
	req := httptest.NewRequest("POST", SABPath, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	result := httptest.NewRecorder()
	s.SAB(result, req)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"status":true`) {
		t.Fatal(result.Code, result.Body.String())
	}
	jobs, err := s.repo.GatewayJobs(context.Background(), s.tenant)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	return jobs[0]
}
func due(t *testing.T, s *Service, conn *pgx.Conn, id string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), "UPDATE gateway_jobs SET next_check_at=now()-interval '1 second' WHERE tenant=$1 AND id=$2", s.tenant, id); err != nil {
		t.Fatal(err)
	}
}
func TestCloudUploadCompletionAndSavedSource(t *testing.T) {
	s, p, conn := testService(t)
	ctx := context.Background()
	job := addTestJob(t, s)
	duplicate := addTestJob(t, s)
	if duplicate.ID != job.ID {
		t.Fatal("duplicate upload lost job identity")
	}
	source, err := s.Source(ctx, job.ID)
	if err != nil || string(source.NZBFile) != testNZB || source.InfoHash != "" || source.DownloadID != job.ID {
		t.Fatal(source, err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if p.creates != 1 {
		t.Fatal("not submitted exactly once")
	}
	ready, err := s.Ready(ctx, job.ID)
	if ready || err != nil {
		t.Fatal("premature completion", ready, err)
	}
	// Simulate the separately mounted DAV file; no FUSE mount or TorBox account is used.
	p.ready = true
	target := filepath.Join(s.cfg.MountPath, "usenet", job.ID, "Release", "Movie & test.mkv")
	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	due(t, s, conn, job.ID)
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	ready, err = s.Ready(ctx, job.ID)
	if !ready || err != nil {
		t.Fatal("not ready", ready, err)
	}
	stored, _ := s.repo.GatewayJobCatalog(ctx, s.tenant, job.ID)
	link := filepath.Join(s.storage(stored), "Release", "Movie & test.mkv")
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatal(got, err)
	}
	// Removing completed queue history must preserve catalog and recovery source.
	if err = s.repo.HideGatewayJob(ctx, s.tenant, job.ID); err != nil {
		t.Fatal(err)
	}
	source, err = s.Source(ctx, job.ID)
	if err != nil || len(source.NZBFile) == 0 {
		t.Fatal("source removed with queue history", err)
	}
	// Three distinct claimed polls confirm absence; recovery retains the stable ID.
	p.exists = false
	for i := 0; i < 3; i++ {
		due(t, s, conn, job.ID)
		if err = s.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	recovered, _ := s.repo.GatewayJobCatalog(ctx, s.tenant, job.ID)
	if recovered.State != "Queued" || recovered.ID != job.ID {
		t.Fatal("recovery not queued", recovered)
	}
	if p.creates != 1 {
		t.Fatal("missing probes submitted prematurely")
	}
}
func TestCloudDAVRangeAndCatalog(t *testing.T) {
	s, p, conn := testService(t)
	ctx := context.Background()
	job := addTestJob(t, s)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	p.ready = true
	due(t, s, conn, job.ID)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials forwarded to CDN")
		}
		if r.Header.Get("Range") != "bytes=1-3" {
			t.Error("range not forwarded")
		}
		w.Header().Set("Content-Range", "bytes 1-3/6")
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(206)
		fmt.Fprint(w, "bcd")
	}))
	defer cdn.Close()
	p.link = cdn.URL
	request := func(method, url string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, nil)
		r.SetBasicAuth("dav", "dav-key")
		r.Header.Set("Range", "bytes=1-3")
		w := httptest.NewRecorder()
		s.DAV(w, r)
		return w
	}
	pf := request("PROPFIND", DAVPath+"usenet/"+job.ID+"/Release/")
	if pf.Code != 207 {
		t.Fatal(pf.Code, pf.Body.String())
	}
	decoder := xml.NewDecoder(strings.NewReader(pf.Body.String()))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("invalid DAV XML", err)
		}
	}
	fileURL := DAVPath + "usenet/" + job.ID + "/Release/Movie%20&%20test.mkv"
	head := request("HEAD", fileURL)
	if head.Code != 200 || head.Header().Get("Content-Length") != "6" || p.links != 0 {
		t.Fatal("HEAD fetched content", head.Code, p.links)
	}
	for i := 0; i < 2; i++ {
		result := request("GET", fileURL)
		if result.Code != 206 || result.Body.String() != "bcd" {
			t.Fatal(result.Code, result.Body.String())
		}
	}
	if p.links != 1 {
		t.Fatal("delivery URL not cached", p.links)
	}
	if got := request("DELETE", fileURL); got.Code != 405 {
		t.Fatal("DAV mutation allowed")
	}
	r := httptest.NewRequest("GET", fileURL, nil)
	w := httptest.NewRecorder()
	s.DAV(w, r)
	if w.Code != 401 {
		t.Fatal("unauthorized DAV allowed")
	}
}
func TestCloudSubmissionInterruptedDoesNotDuplicate(t *testing.T) {
	s, p, conn := testService(t)
	job := addTestJob(t, s)
	if _, err := conn.Exec(context.Background(), "UPDATE gateway_jobs SET state='Submitting',next_check_at=now()-interval '1 second' WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := s.repo.GatewayJobCatalog(context.Background(), s.tenant, job.ID)
	if j.State != "SubmissionUnknown" || p.creates != 0 {
		t.Fatal("uncertain submission duplicated", j, p.creates)
	}
}

type observingNZB struct {
	*fakeProvider
	existing int64
	lookups  int
}

func (p *observingNZB) FindSubmission(ctx context.Context, jobID, filename string) (int64, error) {
	p.lookups++
	return p.existing, nil
}
func TestUnknownNZBRecoveryNeverResubmitsAndRefusesSharedBinding(t *testing.T) {
	s, p, conn := testService(t)
	lookup := &observingNZB{fakeProvider: p}
	s.provider = lookup
	ctx := context.Background()
	j, err := s.repo.AddGatewayNZB(ctx, s.tenant, "radarr", "original.nzb", []byte(testNZB))
	if err != nil {
		t.Fatal(err)
	}
	j.State = "SubmissionUnknown"
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, j, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.State != "SubmissionUnknown" || p.creates != 0 {
		t.Fatal(current, p.creates, err)
	}
	lookup.existing = 4321
	if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE tenant=$1 AND id=$2", s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.State != "Downloading" || current.ProviderID != 4321 || current.Attempts != j.Attempts || p.creates != 0 || lookup.lookups != 2 {
		t.Fatal(current, p.creates, err)
	}
	// A second source must not bind the same provider placement.
	other, err := s.repo.AddGatewayNZB(ctx, s.tenant, "radarr", "other.nzb", []byte(strings.ReplaceAll(testNZB, "article@example", "other@example")))
	if err != nil {
		t.Fatal(err)
	}
	other.State = "SubmissionUnknown"
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, other, 0); err != nil {
		t.Fatal(err)
	}
	bound, err := s.repo.ResolveUnknownGatewayNZB(ctx, s.tenant, other, 4321)
	if err != nil || bound {
		t.Fatal("shared provider binding allowed", err)
	}
	// Cross-tenant attempts cannot resolve the job either.
	bound, err = s.repo.ResolveUnknownGatewayNZB(ctx, "another-tenant", other, 4322)
	if err != nil || bound {
		t.Fatal("cross tenant binding allowed", err)
	}
}

func TestNZBUploadCarriesJobMarkerWithoutChangingSavedSource(t *testing.T) {
	s, p, _ := testService(t)
	j := addTestJob(t, s)
	if err := s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.submissionFilename != torbox.UsenetSubmissionFilename(j.ID, "original.nzb") {
		t.Fatal("missing job identity on provider upload")
	}
	saved, err := s.repo.GatewayJob(context.Background(), s.tenant, j.ID)
	if err != nil || saved.Filename != "original.nzb" || string(saved.NZB) != testNZB {
		t.Fatal("original source changed", err)
	}
}
