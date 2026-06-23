package syncer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/seerr"
)

type SeerrSyncer struct {
	repo     *db.Repo
	seerr    *seerr.Client
	tenant   string
	interval time.Duration
	limit    int
}

type SeerrOptions struct {
	Repo     *db.Repo
	Seerr    *seerr.Client
	Tenant   string
	Interval time.Duration
	Limit    int
}

func NewSeerr(opt SeerrOptions) *SeerrSyncer {
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Minute
	}
	if opt.Limit <= 0 {
		opt.Limit = 100
	}
	return &SeerrSyncer{repo: opt.Repo, seerr: opt.Seerr, tenant: opt.Tenant, interval: opt.Interval, limit: opt.Limit}
}

func (s *SeerrSyncer) Run(ctx context.Context) {
	slog.Info("seerr request sync starting", "tenant", s.tenant, "interval", s.interval, "limit", s.limit)
	_ = s.SyncOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("seerr request sync stopped")
			return
		case <-ticker.C:
			_ = s.SyncOnce(ctx)
		}
	}
}

func (s *SeerrSyncer) SyncOnce(ctx context.Context) error {
	requests, err := s.seerr.Requests(ctx, s.limit)
	if err != nil {
		slog.Error("seerr request sync failed to list requests", "error", err)
		return err
	}

	seen := 0
	newRequests := 0
	rearmRequested := 0
	notTracked := 0

	for _, req := range requests {
		if req.MediaType != model.MediaMovie || req.TMDBID <= 0 {
			continue
		}
		seen++

		state, err := s.repo.UpsertSeerrRequest(ctx, db.SeerrRequestUpsert{
			Tenant:     s.tenant,
			RequestKey: req.RequestKey,
			MediaType:  req.MediaType,
			TMDBID:     req.TMDBID,
			Title:      req.Title,
			Status:     req.Status,
			RawJSON:    req.RawJSON,
		})
		if err != nil {
			slog.Warn("seerr request sync failed to upsert request", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "error", err)
			continue
		}
		if state.IsNew {
			newRequests++
		}

		placeholder, created, err := s.repo.UpsertRequestedMoviePlaceholder(ctx, s.tenant, req.TMDBID, req.Title, req.Status)
		if err != nil {
			slog.Warn("seerr request sync failed to create requested placeholder", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "error", err)
		} else if created {
			payload, _ := json.Marshal(map[string]any{
				"request_key": req.RequestKey,
				"tmdb_id":     req.TMDBID,
				"title":       req.Title,
				"source":      "seerr_sync",
			})
			_ = s.repo.Event(ctx, placeholder.ID, "seerr_requested_placeholder_created", string(payload))
			slog.Info("seerr request created requested placeholder", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "arr_id", placeholder.ArrID, "title", req.Title)
		}

		// Polling is intentionally one-shot per Seerr request key. Persistent request rows
		// should not re-arm every time Rehydrator prunes an item later.
		if !state.IsNew || state.RearmRequestedAt != nil {
			continue
		}

		item, matched, err := s.repo.RequestRearmByTMDB(ctx, s.tenant, req.MediaType, req.TMDBID, false)
		if err != nil {
			slog.Warn("seerr request sync failed to request rearm", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "error", err)
			continue
		}
		if !matched {
			notTracked++
			slog.Info("seerr request is not archived/tracked yet; no rearm requested", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "title", req.Title)
			continue
		}

		if err := s.repo.MarkSeerrRequestRearmed(ctx, s.tenant, req.RequestKey); err != nil {
			slog.Warn("seerr request sync failed to mark request rearmed", "request_key", req.RequestKey, "error", err)
		}
		payload, _ := json.Marshal(map[string]any{
			"request_key": req.RequestKey,
			"tmdb_id":     req.TMDBID,
			"title":       req.Title,
			"source":      "seerr_sync",
		})
		_ = s.repo.Event(ctx, item.ID, "seerr_rearm_requested", string(payload))
		rearmRequested++
		slog.Info("seerr request triggered rearm", "request_key", req.RequestKey, "tmdb_id", req.TMDBID, "arr_id", item.ArrID, "title", req.Title)
	}

	slog.Info("seerr request sync complete", "requests_seen", seen, "new_requests", newRequests, "rearm_requested", rearmRequested, "not_tracked", notTracked)
	return nil
}
