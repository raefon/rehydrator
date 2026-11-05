package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
)

func TestReleaseFolderSanitization(t *testing.T) {
	for _, name := range []string{"Baki.2018-S01E11-Tiger.Slayer.nzb", "Show.S01.1080p.nzb", "Movie.2026.2160p.nzb", "../../escape\\name\x00\n.nzb", "..", "", strings.Repeat("界", 200)} {
		folder := releaseFolder(db.GatewayJob{Name: name})
		if !validReleaseFolder(folder) || len(folder) > 180 || !utf8.ValidString(folder) || strings.HasSuffix(strings.ToLower(folder), ".nzb") {
			t.Fatal(name, folder)
		}
	}
	if got := releaseFolder(db.GatewayJob{Filename: "Baki.2018.S01E11.nzb"}); got != "Baki.2018.S01E11" {
		t.Fatal(got)
	}
}
func TestCompletedNZBUpgradeUsesReleaseFolderWithoutRedownload(t *testing.T) {
	for _, release := range []string{"Baki.2018.S01E11.Tiger.Slayer", "Show.S01.Complete.1080p", "Movie.2026.1080p"} {
		t.Run(release, func(t *testing.T) {
			s, p, conn := testService(t)
			s.SetRole(config.RoleController)
			ctx := context.Background()
			j, err := s.repo.AddGatewayNZB(ctx, s.tenant, "sonarr", release+".nzb", []byte(testNZB))
			if err != nil {
				t.Fatal(err)
			}
			j.Name = release
			j.ProviderID = 101
			j.State = "Completed"
			j.Attempts = 1
			j.Files = []torbox.UsenetFile{{ID: 0, Name: release + ".mkv", Size: 6}}
			if err = s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Minute); err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Exec(ctx, "UPDATE gateway_jobs SET attempts=1 WHERE id=$1", j.ID); err != nil {
				t.Fatal(err)
			}
			j, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(s.cfg.MountPath, "usenet", j.ID, j.Files[0].Name)
			if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(target, []byte("abcdef"), 0600); err != nil {
				t.Fatal(err)
			}
			oldLink := filepath.Join(s.storage(j), j.Files[0].Name)
			if err = os.MkdirAll(filepath.Dir(oldLink), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(target, oldLink); err != nil {
				t.Fatal(err)
			}
			migration, err := os.ReadFile("../../migrations/035_sab_release_folder.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Exec(ctx, string(migration)); err != nil {
				t.Fatal(err)
			}
			j, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil || j.State != "Mounting" {
				t.Fatal(j, err)
			}
			history := func() []struct {
				ID            string `json:"nzo_id"`
				Name, Storage string
			} {
				r := httptest.NewRequest("GET", SABPath+"?mode=history&apikey=sab-key", nil)
				w := httptest.NewRecorder()
				s.SAB(w, r)
				var data struct {
					History struct {
						Slots []struct {
							ID            string `json:"nzo_id"`
							Name, Storage string
						}
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Fatal(err)
				}
				return data.History.Slots
			}
			if len(history()) != 0 {
				t.Fatal("unverified path advertised complete")
			}
			if err = s.ReconcileMount(ctx); err != nil {
				t.Fatal(err)
			}
			j, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil || j.State != "Completed" || j.ReleaseFolder != release || j.Attempts != 1 || p.creates != 0 {
				t.Fatal(j, err)
			}
			slots := history()
			if len(slots) != 1 || slots[0].Storage != filepath.Join(s.cfg.DownloadPath, j.Category, j.ID, release) || slots[0].Name != release {
				t.Fatal(slots)
			}
			link := filepath.Join(slots[0].Storage, j.Files[0].Name)
			if contents, err := os.ReadFile(link); err != nil || string(contents) != "abcdef" {
				t.Fatal(string(contents), err)
			}
			if destination, err := os.Readlink(oldLink); err != nil || destination != target {
				t.Fatal("old link changed", err)
			}
			// Provider naming changes and schema reapplication cannot rename/re-gate the job.
			j.Name = "Provider.Renamed.Release"
			if err = s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Minute); err != nil {
				t.Fatal(err)
			}
			if _, err = conn.Exec(ctx, string(migration)); err != nil {
				t.Fatal(err)
			}
			persisted, err := s.repo.GatewayJob(ctx, s.tenant, j.ID)
			if err != nil || persisted.ReleaseFolder != release || persisted.State != "Completed" || history()[0].Storage != slots[0].Storage {
				t.Fatal(persisted, err)
			}
		})
	}
}

func TestTorrentReleaseNamedImportPaths(t *testing.T) {
	s := &Service{cfg: config.GatewayStorageConfig{DownloadPath: "/downloads"}}
	j := db.GatewayJob{ID: "torrent-uuid", Category: "sonarr", Protocol: "torrent", Name: "Demon.Slayer.S04E02.1080p"}
	// Legacy paths remain usable until the controller publishes a named folder.
	if s.storage(j) != "/downloads/sonarr/torrent-uuid" {
		t.Fatal(s.storage(j))
	}
	j.ReleaseFolder = releaseFolder(j)
	if filepath.Base(s.storage(j)) != j.Name {
		t.Fatal(s.storage(j))
	}
	if filepath.Dir(s.storage(j)) != s.torrentSavePath(j) {
		t.Fatal("content path must be below save path")
	}
	if got := releaseFolder(db.GatewayJob{Filename: "Show.S01E01.torrent"}); got != "Show.S01E01" {
		t.Fatal(got)
	}
}
