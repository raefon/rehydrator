package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raefon/rehydrator/internal/torbox"
)

type ProviderHealth struct {
	Reason    string
	TorrentID int64
	Status    torbox.Availability
	Failures  int
	CheckedAt time.Time
}

func (p *FileHealthPass) ProviderHealth(ctx context.Context, hash string, maxAge time.Duration) (ProviderHealth, bool, error) {
	var h ProviderHealth
	err := p.tx.QueryRow(ctx, `SELECT status,consecutive_failures,checked_at,reason,torrent_id FROM media_cache_provider_health WHERE tenant=$1 AND provider='torbox' AND infohash=$2 AND checked_at>now()-($3*interval '1 second')`, p.tenant, hash, maxAge.Seconds()).Scan(&h.Status, &h.Failures, &h.CheckedAt, &h.Reason, &h.TorrentID)
	if err == pgx.ErrNoRows {
		return h, false, nil
	}
	return h, err == nil, err
}
func (p *FileHealthPass) RecordProviderHealth(ctx context.Context, hash string, observation torbox.Observation, threshold int) (ProviderHealth, error) {
	var h ProviderHealth
	err := p.tx.QueryRow(ctx, `INSERT INTO media_cache_provider_health(tenant,provider,infohash,status,consecutive_failures,reason,torrent_id) VALUES($1,'torbox',$2,$3,CASE WHEN $3='unavailable' THEN 1 ELSE 0 END,$5,$6)
 ON CONFLICT(tenant,provider,infohash) DO UPDATE SET status=EXCLUDED.status,reason=EXCLUDED.reason,torrent_id=EXCLUDED.torrent_id,checked_at=now(),consecutive_failures=CASE WHEN EXCLUDED.status='unavailable' THEN LEAST(media_cache_provider_health.consecutive_failures+1,$4) ELSE 0 END
 RETURNING status,consecutive_failures,checked_at,reason,torrent_id`, p.tenant, hash, observation.Status, threshold, observation.Reason, observation.TorrentID).Scan(&h.Status, &h.Failures, &h.CheckedAt, &h.Reason, &h.TorrentID)
	return h, err
}

type ProviderHealthReport struct {
	InfoHash     string    `json:"infohash"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason"`
	TorrentID    int64     `json:"torrent_id"`
	CheckedAt    time.Time `json:"checked_at"`
	Failures     int       `json:"consecutive_failures"`
	TrackedFiles int       `json:"tracked_files"`
}

func (r *Repo) ProviderHealthReport(ctx context.Context, tenant string, limit int) ([]ProviderHealthReport, error) {
	rows, err := r.pool.Query(ctx, `SELECT p.infohash,p.status,p.reason,p.torrent_id,p.checked_at,p.consecutive_failures,(SELECT count(*) FROM media_cache_state m WHERE m.tenant=p.tenant AND m.source_protocol='torrent' AND m.infohash=p.infohash) FROM media_cache_provider_health p WHERE p.tenant=$1 AND p.provider='torbox' ORDER BY p.checked_at DESC LIMIT $2`, tenant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProviderHealthReport{}
	for rows.Next() {
		var item ProviderHealthReport
		if err := rows.Scan(&item.InfoHash, &item.Status, &item.Reason, &item.TorrentID, &item.CheckedAt, &item.Failures, &item.TrackedFiles); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
