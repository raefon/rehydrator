package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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
               tmdb_id, tvdb_id,
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
               tmdb_id, tvdb_id,
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

type itemRow interface {
	Scan(dest ...any) error
}

const itemSelectColumns = `
    id::text, tenant, media_type, arr_id, symlink_path, state,
    rearm_requested, cached_until, torbox_torrent_id,
    infohash, magnet, download_client, download_category, arr_title, source_title,
    tmdb_id, tvdb_id,
    retry_count, last_checked, last_rehydrated, last_pruned, last_error
`

func scanItem(row itemRow) (model.MediaCacheState, error) {
	var m model.MediaCacheState
	err := row.Scan(
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
		&m.TMDBID,
		&m.TVDBID,
		&m.RetryCount,
		&m.LastChecked,
		&m.LastRehydrated,
		&m.LastPruned,
		&m.LastError,
	)
	return m, err
}

func scanItems(rows itemRows) ([]model.MediaCacheState, error) {
	items := make([]model.MediaCacheState, 0)
	for rows.Next() {
		m, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *Repo) UpsertImportedMovie(ctx context.Context, tenant string, arrID int, title string, symlinkPath string, category string, cachedUntil time.Time, tmdbID int, tvdbID int) (model.MediaCacheState, error) {
	row := r.pool.QueryRow(ctx, `
        INSERT INTO media_cache_state (
            tenant, media_type, arr_id, symlink_path, state, rearm_requested,
            cached_until, download_client, download_category, arr_title, tmdb_id, tvdb_id,
            retry_count, last_error, last_checked
        )
        VALUES ($1, 'movie', $2, $3, 'AVAILABLE', false, $4, 'decypharr', NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, 0), NULLIF($8, 0), 0, NULL, now())
        ON CONFLICT (tenant, media_type, arr_id)
        DO UPDATE SET
            symlink_path = EXCLUDED.symlink_path,
            download_client = 'decypharr',
            download_category = COALESCE(EXCLUDED.download_category, media_cache_state.download_category),
            arr_title = COALESCE(EXCLUDED.arr_title, media_cache_state.arr_title),
            tmdb_id = COALESCE(EXCLUDED.tmdb_id, media_cache_state.tmdb_id),
            tvdb_id = COALESCE(EXCLUDED.tvdb_id, media_cache_state.tvdb_id),
            state = CASE
                WHEN media_cache_state.state = 'REQUESTED' THEN 'AVAILABLE'
                ELSE media_cache_state.state
            END,
            cached_until = CASE
                WHEN media_cache_state.state IN ('REQUESTED', 'AVAILABLE', 'HOT', 'COOLING')
                     AND media_cache_state.cached_until IS NULL THEN EXCLUDED.cached_until
                ELSE media_cache_state.cached_until
            END,
            last_checked = now()
        RETURNING `+itemSelectColumns+`
    `, tenant, arrID, symlinkPath, cachedUntil, category, title, tmdbID, tvdbID)
	return scanItem(row)
}

type SeerrRequestUpsert struct {
	Tenant     string
	RequestKey string
	MediaType  model.MediaType
	TMDBID     int
	ArrID      int
	Title      string
	Status     string
	RawJSON    string
}

type SeerrRequestState struct {
	IsNew            bool
	RearmRequestedAt *time.Time
}

func (r *Repo) UpsertSeerrRequest(ctx context.Context, req SeerrRequestUpsert) (SeerrRequestState, error) {
	var state SeerrRequestState
	var existingRearmedAt *time.Time
	err := r.pool.QueryRow(ctx, `
        SELECT rearm_requested_at
        FROM media_cache_seerr_requests
        WHERE tenant = $1 AND request_key = $2
    `, req.Tenant, req.RequestKey).Scan(&existingRearmedAt)
	if err != nil && err != pgx.ErrNoRows {
		return state, err
	}

	if err == pgx.ErrNoRows {
		_, err = r.pool.Exec(ctx, `
            INSERT INTO media_cache_seerr_requests (
                tenant, request_key, media_type, tmdb_id, arr_id, title, status, raw
            ) VALUES (
                $1, $2, $3, NULLIF($4, 0), NULLIF($5, 0), NULLIF($6, ''), NULLIF($7, ''), COALESCE(NULLIF($8, '')::jsonb, '{}'::jsonb)
            )
        `, req.Tenant, req.RequestKey, req.MediaType, req.TMDBID, req.ArrID, req.Title, req.Status, req.RawJSON)
		state.IsNew = true
		return state, err
	}

	_, err = r.pool.Exec(ctx, `
        UPDATE media_cache_seerr_requests
        SET media_type = $3,
            tmdb_id = COALESCE(NULLIF($4, 0), tmdb_id),
            arr_id = COALESCE(NULLIF($5, 0), arr_id),
            title = COALESCE(NULLIF($6, ''), title),
            status = COALESCE(NULLIF($7, ''), status),
            raw = COALESCE(NULLIF($8, '')::jsonb, raw),
            last_seen_at = now()
        WHERE tenant = $1 AND request_key = $2
    `, req.Tenant, req.RequestKey, req.MediaType, req.TMDBID, req.ArrID, req.Title, req.Status, req.RawJSON)
	state.RearmRequestedAt = existingRearmedAt
	return state, err
}

func (r *Repo) MarkSeerrRequestRearmed(ctx context.Context, tenant string, requestKey string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_seerr_requests
        SET rearm_requested_at = now(), last_seen_at = now()
        WHERE tenant = $1 AND request_key = $2
    `, tenant, requestKey)
	return err
}

func (r *Repo) RequestRearmByTMDB(ctx context.Context, tenant string, mediaType model.MediaType, tmdbID int, force bool) (model.MediaCacheState, bool, error) {
	if tmdbID <= 0 {
		return model.MediaCacheState{}, false, nil
	}

	statePredicate := "AND state IN ('REQUESTED', 'ARCHIVED', 'BROKEN', 'FAILED')"
	if force {
		statePredicate = ""
	}

	row := r.pool.QueryRow(ctx, `
        UPDATE media_cache_state
        SET state = 'ARCHIVED',
            rearm_requested = true,
            cached_until = NULL,
            retry_count = 0,
            last_error = NULL,
            last_checked = now()
        WHERE tenant = $1
          AND media_type = $2
          AND tmdb_id = $3
          `+statePredicate+`
        RETURNING `+itemSelectColumns+`
    `, tenant, mediaType, tmdbID)

	item, err := scanItem(row)
	if err == pgx.ErrNoRows {
		return model.MediaCacheState{}, false, nil
	}
	if err != nil {
		return model.MediaCacheState{}, false, err
	}
	return item, true, nil
}

func (r *Repo) RequestRearmByArrIDIfArchived(ctx context.Context, tenant string, mediaType model.MediaType, arrID int) (model.MediaCacheState, bool, error) {
	row := r.pool.QueryRow(ctx, `
        UPDATE media_cache_state
        SET state = 'ARCHIVED',
            rearm_requested = true,
            cached_until = NULL,
            retry_count = 0,
            last_error = NULL,
            last_checked = now()
        WHERE tenant = $1
          AND media_type = $2
          AND arr_id = $3
          AND state IN ('REQUESTED', 'ARCHIVED', 'BROKEN', 'FAILED')
        RETURNING `+itemSelectColumns+`
    `, tenant, mediaType, arrID)

	item, err := scanItem(row)
	if err == pgx.ErrNoRows {
		return model.MediaCacheState{}, false, nil
	}
	if err != nil {
		return model.MediaCacheState{}, false, err
	}
	return item, true, nil
}

func (r *Repo) RequestRearm(ctx context.Context, tenant string, mediaType model.MediaType, arrID int) (model.MediaCacheState, error) {
	row := r.pool.QueryRow(ctx, `
        UPDATE media_cache_state
        SET state = 'ARCHIVED',
            rearm_requested = true,
            cached_until = NULL,
            retry_count = 0,
            last_error = NULL,
            last_checked = now()
        WHERE tenant = $1
          AND media_type = $2
          AND arr_id = $3
        RETURNING `+itemSelectColumns+`
    `, tenant, mediaType, arrID)
	return scanItem(row)
}

func (r *Repo) ListState(ctx context.Context, tenant string, limit int) ([]model.MediaCacheState, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
        SELECT `+itemSelectColumns+`
        FROM media_cache_state
        WHERE tenant = $1
        ORDER BY updated_at DESC
        LIMIT $2
    `, tenant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
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

func (r *Repo) SaveTorBoxTorrentID(ctx context.Context, id string, torrentID string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET torbox_torrent_id = NULLIF($2, ''),
            last_checked = now()
        WHERE id = $1
    `, id, torrentID)
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
