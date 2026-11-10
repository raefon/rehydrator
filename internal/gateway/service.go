package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/csi"
	"github.com/raefon/rehydrator/internal/dashboardauth"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
)

const Prefix = "cloud-nzb-"

func IsJob(id string) bool { return strings.HasPrefix(strings.ToLower(id), Prefix) }

type Provider interface {
	Create(context.Context, []byte, string) (int64, error)
	Get(context.Context, int64) (torbox.UsenetDownload, bool, error)
	Link(context.Context, int64, int64) (string, error)
}
type TorrentProvider interface {
	Submit(context.Context, model.TorrentMetadata) (int64, error)
	Get(context.Context, int64) (torbox.UsenetDownload, bool, error)
	Link(context.Context, int64, int64) (string, error)
}
type Service struct {
	activePollInterval    time.Duration
	diagnosticMu          sync.Mutex
	diagnosticChecks      map[string]time.Time
	dashboardOIDC         *dashboardauth.Auth
	dashboardExternalAuth bool
	role                  string
	torrent               TorrentProvider
	qbUser, qbPassword    string
	cfg                   config.GatewayStorageConfig
	tenant                string
	repo                  *db.Repo
	provider              Provider
	mountProbe            *csi.ReadProbe
	links                 linkCache
	categories            map[string]bool
}

func newService(cfg config.GatewayStorageConfig, tenant string, repo *db.Repo, provider Provider, categories ...string) *Service {
	allowed := map[string]bool{}
	for _, cat := range categories {
		allowed[cat] = true
	}
	return &Service{activePollInterval: 30 * time.Second, cfg: cfg, tenant: tenant, repo: repo, provider: provider, categories: allowed, mountProbe: csi.NewReadProbeWithLimit(csi.NewChecker(cfg.MountPath), 1)}
}
func (s *Service) Source(ctx context.Context, id string) (model.TorrentMetadata, error) {
	j, err := s.repo.GatewayJob(ctx, s.tenant, strings.ToLower(id))
	if err != nil {
		return model.TorrentMetadata{}, err
	}
	if j.Protocol == model.ProtocolTorrent {
		return (model.TorrentMetadata{Protocol: model.ProtocolTorrent, Kind: model.SourceKind(j.SourceKind), InfoHash: j.InfoHash, Magnet: j.Magnet, TorrentFile: j.TorrentFile, TorrentFilename: j.Filename, DownloadID: j.InfoHash, SourceTitle: j.Name, Source: "torbox_gateway"}).ValidateSource()
	}
	return (model.TorrentMetadata{Protocol: model.ProtocolUsenet, Kind: model.SourceNZBFile, NZBFile: j.NZB, NZBFilename: j.Filename, DownloadID: j.ID, SourceTitle: j.Name, Source: "torbox_gateway"}).ValidateSource()
}
func (s *Service) Ready(ctx context.Context, id string) (bool, error) {
	j, err := s.repo.GatewayJob(ctx, s.tenant, strings.ToLower(id))
	if err != nil {
		return false, err
	}
	if j.State == "Failed" || j.State == "SubmissionUnknown" {
		return false, fmt.Errorf("%w: gateway job %s: %s", model.ErrSourceRecoveryRequired, j.State, j.LastError)
	}
	return j.State == "Completed", nil
}
func (s *Service) Run(ctx context.Context) {
	slog.Info("TorBox gateway worker starting", "mount_path", s.cfg.MountPath, "submit_interval_seconds", s.cfg.SubmitIntervalSeconds, "active_poll_interval_seconds", s.activePollInterval.Seconds())
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, 55*time.Second)
		for i := 0; i < 5 && pass.Err() == nil; i++ {
			if err := s.Reconcile(pass); err != nil {
				slog.Warn("TorBox gateway worker", "error", err)
				break
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) Reconcile(ctx context.Context) error {
	if s.role == config.RoleController {
		return fmt.Errorf("provider reconciliation is disabled in controller role")
	}
	if lookup, ok := s.torrent.(interface {
		FindActive(context.Context, string) (int64, error)
	}); ok {
		j, claimed, err := s.repo.ClaimUnknownGatewayTorrent(ctx, s.tenant)
		if err != nil {
			return err
		}
		if claimed {
			id, err := lookup.FindActive(ctx, j.InfoHash)
			if err != nil {
				slog.Warn("TorBox unknown submission lookup inconclusive", "job_id", j.ID, "error", err)
				return s.repo.RecordUnknownGatewayLookup(ctx, s.tenant, j.ID, "unknown submission lookup inconclusive: "+err.Error())
			}
			if id > 0 {
				resolved, err := s.repo.ResolveUnknownGatewayTorrent(ctx, s.tenant, j, id)
				if err != nil {
					return err
				}
				if resolved {
					slog.Info("TorBox unknown submission matched existing active torrent", "job_id", j.ID, "provider_id", id, "infohash", j.InfoHash)
				}
			}
			return nil
		}
	}

	if lookup, ok := s.provider.(interface {
		FindSubmission(context.Context, string, string) (int64, error)
	}); ok {
		j, claimed, err := s.repo.ClaimUnknownGatewayNZB(ctx, s.tenant)
		if err != nil {
			return err
		}
		if claimed {
			id, err := lookup.FindSubmission(ctx, j.ID, j.Filename)
			if err != nil {
				return s.repo.RecordUnknownGatewayLookup(ctx, s.tenant, j.ID, "NZB submission lookup inconclusive: "+err.Error())
			}
			if id == 0 {
				return s.repo.RecordUnknownGatewayLookup(ctx, s.tenant, j.ID, "No unique NZB submission marker found; no upload performed; legacy submissions require manual review")
			}
			resolved, err := s.repo.ResolveUnknownGatewayNZB(ctx, s.tenant, j, id)
			if err != nil {
				return err
			}
			if resolved {
				slog.Info("TorBox unknown NZB submission matched existing download", "job_id", j.ID, "provider_id", id)
			}
			return nil
		}
	}

	protocol := ""

	j, claimed, err := s.repo.ClaimGatewayJob(ctx, s.tenant, time.Duration(s.cfg.SubmitIntervalSeconds)*time.Second, protocol)
	if err != nil || !claimed {
		return err
	}
	if j.State == "Queued" {
		var id int64
		var err error
		if j.Protocol == model.ProtocolTorrent {
			source, e := s.Source(ctx, j.ID)
			err = e
			if err == nil {
				id, err = s.torrent.Submit(ctx, source)
			}
		} else {
			id, err = s.provider.Create(ctx, j.NZB, torbox.UsenetSubmissionFilename(j.ID, j.Filename))
		}
		if errors.Is(err, torbox.ErrSubmissionNotStarted) {
			slog.Warn("TorBox gateway submission deferred before add", "tenant", s.tenant, "job_id", j.ID, "error", err)
			return s.repo.DeferGatewaySubmission(ctx, s.tenant, j.ID, err.Error())
		}
		if err != nil {
			j.State = "SubmissionUnknown"
			j.LastError = "cloud add outcome unknown; inspect TorBox before retrying: " + err.Error()
		} else {
			j.ProviderID = id
			j.State = "Downloading"
			j.LastError = ""
			j.MissingChecks = 0
		}
		slog.Info("TorBox gateway submission result", "protocol", j.Protocol, "job_id", j.ID, "provider_id", j.ProviderID, "state", j.State)
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, s.activePollInterval)
	}
	var download torbox.UsenetDownload
	var found bool
	if j.Protocol == model.ProtocolTorrent {
		download, found, err = s.torrent.Get(ctx, j.ProviderID)
	} else {
		download, found, err = s.provider.Get(ctx, j.ProviderID)
	}
	if err != nil {
		if recordErr := s.repo.RecordProviderCheckFailure(ctx, s.tenant, j); recordErr != nil {
			return recordErr
		}
		j.LastError = err.Error()
		j.MissingChecks = 0
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, 5*time.Minute)
	}
	if err = s.repo.RecordProviderStatus(ctx, s.tenant, j, download.Status(j.ProviderID, found)); err != nil {
		return err
	}
	if message, failed := download.FailureMessage(); found && failed {
		j.State = "Failed"
		j.LastError = message
		j.MissingChecks = 0
		slog.Warn("TorBox terminal download failure reported to Arr", "tenant", s.tenant, "job_id", j.ID, "protocol", j.Protocol, "provider_id", j.ProviderID)
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Hour)
	}
	if !found || (download.Present != nil && !*download.Present && (download.State == "cached" || download.State == "completed")) {
		if j.MissingChecks < 3 {
			j.MissingChecks++
		}
		j.State = "Missing"
		j.LastError = "TorBox cloud placement is absent; awaiting confirmation"
		if j.MissingChecks >= 3 && s.cfg.RecoverMissing {
			if j.Attempts >= 3 {
				j.State = "Failed"
				j.LastError = "TorBox placement confirmed missing after 3 checks; cloud release recovery attempt limit reached"
				slog.Warn("Confirmed missing download exhausted recovery; failure reported to Arr", "tenant", s.tenant, "job_id", j.ID, "protocol", j.Protocol, "attempts", j.Attempts)
			} else {
				j.State = "Queued"
				j.LastError = "confirmed missing cloud placement; saved source queued for recovery"
				if err := s.repo.GatewayRecoveryWaiting(ctx, s.tenant, j.ID); err != nil {
					return err
				}

			}
		}
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, 5*time.Minute)
	}
	j.MissingChecks = 0
	if download.Name != "" {
		j.Name = download.Name
	}
	if !download.Ready() {
		j.State = "Downloading"
		j.LastError = ""
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, s.activePollInterval)
	}
	if err = torbox.ValidateUsenetFiles(download.Files); err != nil {
		return err
	}
	if len(j.Files) > 0 && !sameCatalog(j.Files, download.Files) {
		j.State = "Failed"
		j.LastError = "provider catalog changed; original import paths cannot be restored safely"
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Hour)
	}
	if j.State == "Completed" {
		j.Files = download.Files
		j.LastError = ""
		return s.repo.UpdateGatewayJob(ctx, s.tenant, j, 15*time.Minute)
	}
	j.Files = download.Files
	j.State = "Mounting"
	j.LastError = ""
	// Publish the virtual catalog before checking its rclone mount.
	if err = s.repo.UpdateGatewayJob(ctx, s.tenant, j, time.Minute); err != nil {
		return err
	}
	if s.role == config.RoleGateway {
		return nil
	}
	// Reload the publication revision used by the controller CAS.
	j, err = s.repo.GatewayJob(ctx, s.tenant, j.ID)
	if err != nil {
		return err
	}
	return s.verifyMount(ctx, j)
}

func (s *Service) verifyMount(ctx context.Context, j db.GatewayJob) error {
	var err error
	if j.ReleaseFolder == "" {
		j, err = s.repo.PinGatewayReleaseFolder(ctx, s.tenant, j, releaseFolder(j))
		if err != nil {
			return err
		}
	}
	if !validReleaseFolder(j.ReleaseFolder) {
		return fmt.Errorf("invalid persisted release folder")
	}

	if !j.Hidden {
		if err := s.createLinks(j); err != nil {
			j.LastError = err.Error()
			_, err := s.repo.FinishGatewayMount(ctx, s.tenant, j, j.LastError)
			return err
		}
	}
	// One bounded CDN byte probe establishes that the delivery mount works.
	// Arr performs its own validation/import of the complete file list.
	candidate := ""
	for _, file := range j.Files {
		if file.Size == 0 {
			continue
		}
		if candidate == "" {
			candidate = file.Name
		}
		switch strings.ToLower(filepath.Ext(file.Name)) {
		case ".mkv", ".mp4", ".m4v", ".avi", ".ts", ".webm":
			candidate = file.Name
		}
		if candidate == file.Name && (strings.EqualFold(filepath.Ext(file.Name), ".mkv") || strings.EqualFold(filepath.Ext(file.Name), ".mp4")) {
			break
		}
	}
	if candidate == "" {
		j.LastError = "waiting for a nonempty cloud payload file"
		_, err := s.repo.FinishGatewayMount(ctx, s.tenant, j, j.LastError)
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	probePath := filepath.Join(s.cfg.MountPath, s.catalogPath(j), filepath.FromSlash(candidate))
	if j.Protocol == model.ProtocolUsenet && !j.Hidden {
		probePath = filepath.Join(s.storage(j), filepath.FromSlash(candidate))
	}
	readable, err := s.mountProbe.Check(probeCtx, probePath)
	cancel()
	if err != nil || !readable {
		j.LastError = "waiting for cloud WebDAV mount visibility/readability"
		_, err := s.repo.FinishGatewayMount(ctx, s.tenant, j, j.LastError)
		return err
	}
	j.State = "Completed"
	j.LastError = ""
	var completed bool
	completed, err = s.repo.FinishGatewayMount(ctx, s.tenant, j, "")
	if completed {
		slog.Info("TorBox gateway ready for Arr import", "protocol", j.Protocol, "job_id", j.ID, "provider_id", j.ProviderID, "files", len(j.Files))
	}
	return err
}

func (s *Service) storage(j db.GatewayJob) string {
	root := filepath.Join(s.cfg.DownloadPath, j.Category, j.ID)
	if j.ReleaseFolder != "" {
		return filepath.Join(root, j.ReleaseFolder)
	}
	return root
}
func (s *Service) createLinks(j db.GatewayJob) error {
	if !s.categories[j.Category] {
		return fmt.Errorf("unconfigured cloud category")
	}
	for _, f := range j.Files {
		dest := filepath.Join(s.storage(j), filepath.FromSlash(f.Name))
		target := filepath.Join(s.cfg.MountPath, s.catalogPath(j), filepath.FromSlash(f.Name))
		if err := safeStagingParents(s.cfg.DownloadPath, filepath.Dir(dest)); err != nil {
			return err
		}
		existing, err := os.Readlink(dest)
		if err == nil {
			if existing != target {
				return fmt.Errorf("cloud download path has a different symlink")
			}
			continue
		}
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("cloud download path already exists")
		}
		if err := os.Symlink(target, dest); err != nil {
			return err
		}
	}
	return nil
}

// Only the configured root is trusted; provider-supplied subdirectories must
// never traverse existing symlinks in the local download tree.
func safeStagingParents(root, directory string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("download path escaped configured root")
	}
	current := root
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("download parent is not a real directory")
		}
	}
	return nil
}

func New(cfg config.GatewayConfig, tenant string, repo *db.Repo, torrents TorrentProvider, usenet Provider, categories ...string) *Service {
	s := newService(cfg.GatewayStorageConfig, tenant, repo, usenet, categories...)

	s.activePollInterval = 30 * time.Second
	if cfg.ActivePollIntervalSeconds >= 15 && cfg.ActivePollIntervalSeconds <= 3600 {
		s.activePollInterval = time.Duration(cfg.ActivePollIntervalSeconds) * time.Second
	}
	s.torrent = torrents
	s.dashboardExternalAuth = cfg.DashboardExternalAuth
	s.qbUser = cfg.QBUsername
	s.qbPassword = cfg.QBPassword
	return s
}
func (s *Service) catalogPath(j db.GatewayJob) string {

	if j.Protocol == model.ProtocolTorrent {
		return "torrents/" + j.ID
	}
	return "usenet/" + j.ID
}
func (s *Service) DAVRoot() string { return DAVPath }

func (s *Service) Ensure(ctx context.Context, source model.TorrentMetadata, category string) (string, error) {
	j, err := s.repo.AddGatewaySource(ctx, s.tenant, category, source)
	if err != nil {
		return "", err
	}
	if err = s.repo.WakeGatewayJob(ctx, s.tenant, j.ID); err != nil {
		return "", err
	}
	if j.Protocol == model.ProtocolTorrent {
		return j.InfoHash, nil
	}
	return j.ID, nil
}

// SourceForImport binds an imported link to its retained gateway job even after
// Arr removes completed download history. It reads link metadata, not media.
func (s *Service) SourceForImport(ctx context.Context, item model.MediaCacheState) (model.TorrentMetadata, error) {
	target, err := os.Readlink(item.SymlinkPath)
	if err != nil {
		return model.TorrentMetadata{}, err
	}
	relative, err := filepath.Rel(s.cfg.MountPath, target)
	if err != nil {
		return model.TorrentMetadata{}, err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 3 || (parts[0] != "torrents" && parts[0] != "usenet") {
		return model.TorrentMetadata{}, model.ErrSourceRecoveryRequired
	}
	j, err := s.repo.GatewayJob(ctx, s.tenant, parts[1])
	if err != nil {
		return model.TorrentMetadata{}, err
	}
	if s.catalogPath(j) != parts[0]+"/"+parts[1] {
		return model.TorrentMetadata{}, model.ErrSourceRecoveryRequired
	}
	return s.Source(ctx, j.ID)
}

func sameCatalog(before, after []torbox.UsenetFile) bool {
	if len(before) != len(after) {
		return false
	}
	names := map[string]int64{}
	for _, f := range before {
		names[f.Name] = f.Size
	}
	for _, f := range after {
		size, ok := names[f.Name]
		if !ok || size != f.Size {
			return false
		}
	}
	return true
}

// SetRole selects split responsibilities. Combined remains the default.
func (s *Service) SetRole(role string) { s.role = role }
func (s *Service) RunMountWorker(ctx context.Context) {
	slog.Info("gateway mount verification worker starting", "tenant", s.tenant, "mount_path", s.cfg.MountPath)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 5 && ctx.Err() == nil; i++ {
			if err := s.ReconcileMount(ctx); err != nil {
				slog.Warn("gateway mount verification failed", "error", err)
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) ReconcileMount(ctx context.Context) error {
	if s.role == config.RoleGateway {
		return fmt.Errorf("filesystem verification is disabled in gateway role")
	}
	j, ok, err := s.repo.ClaimGatewayMount(ctx, s.tenant)
	if err != nil || !ok {
		return err
	}
	if j.CleanupRequested {
		if err = s.removeStagedLinks(j); err != nil {
			return err
		}
		return s.repo.FinishGatewayCleanup(ctx, s.tenant, j)
	}
	return s.verifyMount(ctx, j)
}
