package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/torbox"
)

type pruneRejection string

func (e pruneRejection) Error() string { return string(e) }

type deletionProvider interface {
	Get(context.Context, int64) (torbox.UsenetDownload, bool, error)
	Delete(context.Context, int64) error
}

func (s *Service) deleteFailedPlacement(ctx context.Context, j db.GatewayJob) error {
	if j.ProviderID == 0 {
		return nil
	}
	var p deletionProvider
	var ok bool
	if j.Protocol == "torrent" {
		p, ok = s.torrent.(deletionProvider)
	} else {
		p, ok = s.provider.(deletionProvider)
	}
	if !ok {
		return pruneRejection("provider deletion unavailable")
	}
	download, found, err := p.Get(ctx, j.ProviderID)
	if err != nil {
		return pruneRejection("provider status could not be verified; job retained")
	}
	if !found {
		return nil
	}
	if _, failed := download.FailureMessage(); !failed {
		return pruneRejection("provider download is no longer failed; job retained")
	}
	if err = p.Delete(ctx, j.ProviderID); err != nil {
		return pruneRejection("TorBox deletion was not confirmed; job retained")
	}
	return nil
}
func (s *Service) dashboardPrune(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var body struct {
		IDs     []string `json:"ids"`
		Confirm bool     `json:"confirm"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || !body.Confirm || len(body.IDs) == 0 || len(body.IDs) > 50 {
		http.Error(w, "confirm deletion of 1–50 failed jobs", 400)
		return
	}
	type result struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		Error   string `json:"error,omitempty"`
	}
	results := []result{}
	seen := map[string]bool{}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	for _, id := range body.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		err := s.repo.PruneFailedGatewayJob(ctx, s.tenant, id, func(j db.GatewayJob) error { return s.deleteFailedPlacement(ctx, j) })
		entry := result{ID: id, Deleted: err == nil}
		if err != nil {
			slog.Warn("Failed job deletion blocked or unconfirmed", "tenant", s.tenant, "job_id", id)
			var rejection pruneRejection
			switch {
			case errors.As(err, &rejection):
				entry.Error = rejection.Error()
			case errors.Is(err, db.ErrGatewayPruneInUse):
				entry.Error = "Linked to library media or another job shares this provider download; retained."
			case errors.Is(err, db.ErrGatewayPruneActive):
				entry.Error = "Job is no longer failed or has pending cleanup; retained."
			case errors.Is(err, db.ErrGatewayPruneUnavailable):
				entry.Error = "Job unavailable or already removed."
			default:
				entry.Error = "Database operation failed; refresh and retry."
			}
		} else {
			slog.Info("Failed job pruned after provider deletion check", "tenant", s.tenant, "job_id", id)
		}
		results = append(results, entry)
	}
	writeJSON(w, map[string]any{"results": results})
}
