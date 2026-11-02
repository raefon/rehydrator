package gateway

import (
	"context"
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/model"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGatewayPublishesWithoutMountAndControllerCompletes(t *testing.T) {
	s, p, conn := testService(t)
	s.SetRole(config.RoleGateway)
	ctx := context.Background()
	j := addTestJob(t, s)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	p.ready = true
	due := func() {
		t.Helper()
		if _, err := conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now(),verify_next_at=now() WHERE id=$1", j.ID); err != nil {
			t.Fatal(err)
		}
	}
	due()
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.State != "Mounting" {
		t.Fatal(current, err)
	}
	if _, err = os.Stat(s.cfg.DownloadPath); !os.IsNotExist(err) {
		t.Fatal("gateway accessed staging", err)
	}
	if _, err = os.Stat(s.cfg.MountPath); !os.IsNotExist(err) {
		t.Fatal("gateway accessed CSI", err)
	}
	// DAV metadata is already usable by CSI before a mounted controller exists.
	r := httptest.NewRequest("PROPFIND", "/webdav/usenet/"+j.ID+"/", nil)
	r.SetBasicAuth(s.cfg.DAVUsername, s.cfg.DAVPassword)
	w := httptest.NewRecorder()
	s.DAV(w, r)
	if w.Code != 207 {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg := config.GatewayConfig{GatewayStorageConfig: s.cfg}
	controller := New(cfg, s.tenant, s.repo, nil, nil, "sonarr", "radarr")
	controller.SetRole(config.RoleController)
	if err = controller.ReconcileMount(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if current.State != "Mounting" || p.creates != 1 {
		t.Fatal("missing mount completed or resubmitted", current, p.creates)
	}
	target := filepath.Join(s.cfg.MountPath, "usenet", j.ID, "Release", "Movie & test.mkv")
	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("abcdef"), 0644); err != nil {
		t.Fatal(err)
	}
	due()
	if err = controller.ReconcileMount(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if current.State != "Completed" {
		t.Fatal(current)
	}
	stage := filepath.Join(s.storage(current), "Release", "Movie & test.mkv")
	if link, err := os.Readlink(stage); err != nil || link != target {
		t.Fatal(link, err)
	}
	// A late provider poll from the prior Mounting snapshot cannot undo completion.
	stale := current
	stale.State = "Mounting"
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, stale, 0); err != nil {
		t.Fatal(err)
	}
	current, _ = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if current.State != "Completed" {
		t.Fatal("provider poll undid verified completion", current)
	}
	// Gateway cleanup is durable and performed only by a mounted controller.
	if err = s.repo.RequestGatewayCleanup(ctx, s.tenant, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(stage); err != nil {
		t.Fatal("gateway removed staging locally", err)
	}
	if err = controller.ReconcileMount(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatal("controller did not remove staging", err)
	}
	if _, err = os.Stat(target); err != nil {
		t.Fatal("cleanup removed mounted content", err)
	}
	if _, err = s.Source(ctx, j.ID); err != nil {
		t.Fatal("cleanup lost source", err)
	}
}
func TestStaleMountVerificationCannotCompleteReplacement(t *testing.T) {
	s, _, _ := testService(t)
	ctx := context.Background()
	j := addTestJob(t, s)
	j.State = "Mounting"
	j.ProviderID = 123
	if err := s.repo.UpdateGatewayJob(ctx, s.tenant, j, 0); err != nil {
		t.Fatal(err)
	}
	old, ok, err := s.repo.ClaimGatewayMount(ctx, s.tenant)
	if err != nil || !ok {
		t.Fatal(old, ok, err)
	}
	if _, again, err := s.repo.ClaimGatewayMount(ctx, s.tenant); err != nil || again {
		t.Fatal("duplicate verification lease", again, err)
	}
	replacement := old
	replacement.ProviderID = 456
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, replacement, 0); err != nil {
		t.Fatal(err)
	}
	if completed, err := s.repo.FinishGatewayMount(ctx, s.tenant, old, ""); err != nil || completed {
		t.Fatal("stale placement completed", completed, err)
	}
	current, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.ProviderID != 456 || current.State != "Mounting" {
		t.Fatal(current, err)
	}
}

type observingTorrent struct {
	*fakeTorrent
	existing int64
	lookups  int
}

func (p *observingTorrent) FindActive(ctx context.Context, hash string) (int64, error) {
	p.lookups++
	return p.existing, nil
}
func TestUnknownTorrentAdoptsOnlyExistingPlacementWithoutResubmission(t *testing.T) {
	s, p, conn := testService(t)
	lookup := &observingTorrent{fakeTorrent: &fakeTorrent{fakeProvider: p}}
	s.torrent = lookup
	ctx := context.Background()
	j, err := s.repo.AddGatewaySource(ctx, s.tenant, "sonarr", model.TorrentMetadata{Kind: model.SourceMagnet, Magnet: testMagnet})
	if err != nil {
		t.Fatal(err)
	}
	j.State = "SubmissionUnknown"
	j.LastError = "add outcome unknown"
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
	if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now() WHERE id=$1", j.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	current, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil || current.State != "Downloading" || current.ProviderID != 4321 || current.Attempts != j.Attempts || p.creates != 0 || lookup.lookups != 2 {
		t.Fatal(current, lookup.lookups, p.creates, err)
	}
}
