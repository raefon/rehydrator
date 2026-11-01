package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
)

type recoveryDetail struct {
	Job            dashboardJob          `json:"job"`
	InfoHash       string                `json:"infohash"`
	ProviderID     int64                 `json:"providerId"`
	SourceKind     string                `json:"sourceKind"`
	SourceRetained bool                  `json:"sourceRetained"`
	MissingChecks  int                   `json:"missingChecks"`
	RearmAvailable bool                  `json:"rearmAvailable"`
	Reason         string                `json:"reason"`
	Database       db.GatewayDiagnostics `json:"database"`
}

func (s *Service) recoveryReason(ctx context.Context, j db.CloudNZBJob) (bool, bool, string) {
	_, err := s.Source(ctx, j.ID)
	retained := err == nil
	if !retained {
		return false, false, "Original source unavailable or invalid; acquire a replacement through Sonarr/Radarr."
	}
	if j.State == "SubmissionUnknown" || j.State == "Submitting" {
		return true, false, "Submission outcome is uncertain. Reconcile with TorBox before another upload."
	}
	if j.Attempts >= 3 {
		return true, false, "Submission attempt limit reached. Acquire a replacement through Sonarr/Radarr."
	}
	if j.State == "Failed" {
		return true, false, "Job failed. Acquire a replacement through Sonarr/Radarr; manual rearm cannot reset this failure."
	}
	if !s.cfg.RecoverMissing {
		return true, false, "Provider recovery is disabled by configuration."
	}
	if j.CleanupRequested {
		return true, false, "Staging cleanup is pending."
	}
	if j.State != "Missing" || j.MissingChecks < 3 || j.ProviderID <= 0 {
		return true, false, "Rearm requires three scheduled confirmations of a missing provider placement. Check now is observation only."
	}
	return true, true, "Confirmed missing placement; retained source can be recovered by the normal worker."
}
func (s *Service) dashboardRecovery(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/dashboard/api/recovery/")
	if (action == "lookup" && r.Method != "GET") || (action != "lookup" && r.Method != "POST") {
		http.Error(w, "method not allowed", 405)
		return
	}
	if action != "lookup" && action != "check" && action != "rearm" {
		http.NotFound(w, r)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("id")))
	if len(query) < 1 || len(query) > 128 {
		http.Error(w, "job ID or torrent infohash required", 400)
		return
	}
	j, err := s.repo.GatewayJob(r.Context(), s.tenant, query)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "No tracked gateway job matches this ID or hash.", 404)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if action == "lookup" {
		diagnostics, err := s.repo.GatewayDiagnostics(ctx, s.tenant, j)
		if err != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		retained, allowed, reason := s.recoveryReason(ctx, j)
		writeJSON(w, recoveryDetail{Job: s.dashboardJob(j), InfoHash: j.InfoHash, ProviderID: j.ProviderID, SourceKind: j.SourceKind, SourceRetained: retained, MissingChecks: j.MissingChecks, RearmAvailable: allowed, Reason: reason, Database: diagnostics})
		return
	}
	if action == "rearm" {
		_, allowed, reason := s.recoveryReason(ctx, j)
		if !allowed {
			http.Error(w, reason, 409)
			return
		}
		changed, err := s.repo.RequestGatewayRecovery(ctx, s.tenant, j)
		if err != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		if !changed {
			http.Error(w, "Job changed; reload diagnostics before requesting recovery.", 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		writeJSON(w, map[string]string{"message": "Recovery check scheduled. The worker will recheck TorBox and use normal recovery limits; no upload was forced."})
		return
	}
	s.diagnosticMu.Lock()
	now := time.Now()
	if s.diagnosticChecks == nil {
		s.diagnosticChecks = map[string]time.Time{}
	}
	for id, at := range s.diagnosticChecks {
		if now.Sub(at) >= 15*time.Second {
			delete(s.diagnosticChecks, id)
		}
	}
	if _, busy := s.diagnosticChecks[j.ID]; busy || len(s.diagnosticChecks) >= 10 {
		s.diagnosticMu.Unlock()
		w.Header().Set("Retry-After", "15")
		http.Error(w, "Please wait 15 seconds before another provider check.", 429)
		return
	}
	s.diagnosticChecks[j.ID] = now
	s.diagnosticMu.Unlock()
	// An unknown torrent may have an existing active ID; lookup is read-only.
	id := j.ProviderID
	if id == 0 && j.Protocol == "torrent" {
		if lookup, ok := s.torrent.(interface {
			FindActive(context.Context, string) (int64, error)
		}); ok {
			id, err = lookup.FindActive(ctx, j.InfoHash)
		}
	}
	if err != nil {
		http.Error(w, "Provider lookup inconclusive. No database state or submission budget changed.", 502)
		return
	}
	if id <= 0 {
		writeJSON(w, map[string]any{"message": "No active provider ID established. No upload performed; queued or unknown submissions need reconciliation.", "found": false, "conclusive": false})
		return
	}
	var download torbox.UsenetDownload
	var found bool
	if j.Protocol == "torrent" && s.torrent != nil {
		download, found, err = s.torrent.Get(ctx, id)
	} else if j.Protocol == "usenet" && s.provider != nil {
		download, found, err = s.provider.Get(ctx, id)
	} else {
		http.Error(w, "provider unavailable", 503)
		return
	}
	if err != nil {
		http.Error(w, "Provider lookup inconclusive. No database state or submission budget changed.", 502)
		return
	}
	message := "Placement not found in provider response. Scheduled confirmations are still required before recovery."
	if found {
		message = "Placement exists but is not ready."
		if download.Ready() {
			message = "Placement is ready on TorBox."
		}
	}
	writeJSON(w, map[string]any{"message": message, "found": found, "ready": found && download.Ready(), "providerId": id, "conclusive": true, "checkedAt": time.Now().UTC()})
}
