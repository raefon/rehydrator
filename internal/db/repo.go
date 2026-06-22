package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raefon/rehydrator/internal/model"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, url string) (*Repo, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Repo{pool: pool}, nil
}

func (r *Repo) Close() {
	r.pool.Close()
}

func (r *Repo) InitSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, schemaSQL)
	return err
}

func (r *Repo) RearmWorkItems(ctx context.Context, limit int, maxRetries int) ([]model.MediaCacheState, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id::text, tenant, media_type, arr_id, symlink_path, state,
               rearm_requested, cached_until, torbox_torrent_id,
               infohash, magnet, download_client, download_category, arr_title, source_title,
               retry_count, last_checked, last_rehydrated, last_pruned, last_error
        FROM media_cache_state
        WHERE rearm_requested = true
          AND state IN ('REQUESTED', 'ARCHIVED', 'BROKEN', 'FAILED')
          AND retry_count < $1
        ORDER BY updated_at ASC
        LIMIT $2
    `, maxRetries, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (r *Repo) PruneWorkItems(ctx context.Context, limit int) ([]model.MediaCacheState, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id::text, tenant, media_type, arr_id, symlink_path, state,
               rearm_requested, cached_until, torbox_torrent_id,
               infohash, magnet, download_client, download_category, arr_title, source_title,
               retry_count, last_checked, last_rehydrated, last_pruned, last_error
        FROM media_cache_state
        WHERE state = 'AVAILABLE'
          AND rearm_requested = false
          AND cached_until IS NOT NULL
          AND cached_until < now()
          AND infohash IS NOT NULL
          AND infohash <> ''
        ORDER BY cached_until ASC
        LIMIT $1
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

type itemRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanItems(rows itemRows) ([]model.MediaCacheState, error) {
	items := make([]model.MediaCacheState, 0)
	for rows.Next() {
		var m model.MediaCacheState
		if err := rows.Scan(
			&m.ID,
			&m.Tenant,
			&m.MediaType,
			&m.ArrID,
			&m.SymlinkPath,
			&m.State,
			&m.RearmRequested,
			&m.CachedUntil,
			&m.TorBoxTorrentID,
			&m.InfoHash,
			&m.Magnet,
			&m.DownloadClient,
			&m.DownloadCategory,
			&m.ArrTitle,
			&m.SourceTitle,
			&m.RetryCount,
			&m.LastChecked,
			&m.LastRehydrated,
			&m.LastPruned,
			&m.LastError,
		); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *Repo) MarkRearming(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'REARMING',
            last_checked = now(),
            last_error = NULL
        WHERE id = $1
    `, id)
	return err
}

func (r *Repo) SaveTorrentMetadata(ctx context.Context, id string, torrent model.TorrentMetadata, category string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET infohash = NULLIF($2, ''),
            magnet = NULLIF($3, ''),
            download_client = 'decypharr',
            download_category = NULLIF($4, ''),
            source_title = NULLIF($5, ''),
            last_checked = now()
        WHERE id = $1
    `, id, torrent.InfoHash, torrent.Magnet, category, torrent.SourceTitle)
	return err
}

func (r *Repo) MarkAvailable(ctx context.Context, id string, infoHash string, cachedUntil time.Time) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'AVAILABLE',
            rearm_requested = false,
            cached_until = $2,
            infohash = COALESCE(NULLIF($3, ''), infohash),
            torbox_torrent_id = NULL,
            last_checked = now(),
            last_rehydrated = now(),
            retry_count = 0,
            last_error = NULL
        WHERE id = $1
    `, id, cachedUntil, infoHash)
	return err
}

func (r *Repo) MarkPruning(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'PRUNING',
            last_checked = now(),
            last_error = NULL
        WHERE id = $1
    `, id)
	return err
}

func (r *Repo) MarkArchived(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'ARCHIVED',
            rearm_requested = false,
            torbox_torrent_id = NULL,
            cached_until = NULL,
            last_checked = now(),
            last_pruned = now(),
            retry_count = 0,
            last_error = NULL
        WHERE id = $1
    `, id)
	return err
}

func (r *Repo) MarkFailed(ctx context.Context, id string, msg string, maxRetries int) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = CASE WHEN retry_count + 1 >= $3 THEN 'FAILED' ELSE 'BROKEN' END,
            retry_count = retry_count + 1,
            last_checked = now(),
            last_error = $2
        WHERE id = $1
    `, id, msg, maxRetries)
	return err
}

func (r *Repo) Event(ctx context.Context, mediaID string, eventType string, metadata string) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_events (media_id, event_type, metadata)
        VALUES ($1, $2, $3::jsonb)
    `, mediaID, eventType, metadata)
	return err
}
