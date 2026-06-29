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

type MetricsSnapshot struct {
	Tenant                  string
	ItemsByState            map[string]int64
	SeerrRequests           int64
	SeerrRearmed            int64
	EventsTotal             int64
	FailedItems             int64
	RearmRequested          int64
	ExpiredPruneQueued      int64
	PlaybackIntentRows      int64
	PlaybackIntentTotal     int64
	UnmatchedPlaybackTotal  int64
	UnmatchedPlaybackOpen   int64
	PlaybackIgnoredTotal    int64
	WaitingVisibilityItems  int64
	ProviderCooldownsActive int64
	PlexRefreshTotal        int64
	PlexRefreshFailures     int64
}

type ProviderCooldown struct {
	Tenant        string
	Provider      string
	CooldownUntil time.Time
	Reason        string
	UpdatedAt     time.Time
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

func (r *Repo) Close() { r.pool.Close() }

func (r *Repo) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *Repo) InitSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, schemaSQL)
	return err
}

const itemSelectColumns = `
    id::text, tenant, media_type, arr_id, symlink_path, state,
    rearm_requested, cached_until, torbox_torrent_id,
    infohash, magnet, download_client, download_category, arr_title, source_title,
    tmdb_id, tvdb_id,
    retry_count, next_retry_at, last_play_intent_at, play_intent_count, last_checked, last_rehydrated, last_pruned, last_error
`

type itemRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

type itemRow interface{ Scan(dest ...any) error }

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
		&m.NextRetryAt,
		&m.LastPlayIntentAt,
		&m.PlayIntentCount,
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

// RearmWorkItems atomically claims re-arm work with SKIP LOCKED so multiple
// workers/pods cannot process the same media row concurrently.
func (r *Repo) RearmWorkItems(ctx context.Context, limit int, maxRetries int) ([]model.MediaCacheState, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.pool.Query(ctx, `
        WITH candidates AS (
            SELECT id
            FROM media_cache_state
            WHERE rearm_requested = true
              AND state IN ('REQUESTED', 'ARCHIVED', 'BROKEN', 'FAILED')
              AND retry_count < $1
              AND (next_retry_at IS NULL OR next_retry_at <= now())
            ORDER BY updated_at ASC
            LIMIT $2
            FOR UPDATE SKIP LOCKED
        )
        UPDATE media_cache_state m
        SET state = 'REARMING',
            last_checked = now(),
            last_error = NULL
        FROM candidates
        WHERE m.id = candidates.id
        RETURNING
            m.id::text, m.tenant, m.media_type, m.arr_id, m.symlink_path, m.state,
            m.rearm_requested, m.cached_until, m.torbox_torrent_id,
            m.infohash, m.magnet, m.download_client, m.download_category, m.arr_title, m.source_title,
            m.tmdb_id, m.tvdb_id,
            m.retry_count, m.next_retry_at, m.last_play_intent_at, m.play_intent_count, m.last_checked, m.last_rehydrated, m.last_pruned, m.last_error
    `, maxRetries, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

// PruneWorkItems atomically claims prune work with SKIP LOCKED.
func (r *Repo) PruneWorkItems(ctx context.Context, limit int) ([]model.MediaCacheState, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.pool.Query(ctx, `
        WITH candidates AS (
            SELECT id
            FROM media_cache_state
            WHERE state = 'AVAILABLE'
              AND rearm_requested = false
              AND cached_until IS NOT NULL
              AND cached_until < now()
              AND infohash IS NOT NULL
              AND infohash <> ''
            ORDER BY cached_until ASC
            LIMIT $1
            FOR UPDATE SKIP LOCKED
        )
        UPDATE media_cache_state m
        SET state = 'PRUNING',
            last_checked = now(),
            last_error = NULL
        FROM candidates
        WHERE m.id = candidates.id
        RETURNING
            m.id::text, m.tenant, m.media_type, m.arr_id, m.symlink_path, m.state,
            m.rearm_requested, m.cached_until, m.torbox_torrent_id,
            m.infohash, m.magnet, m.download_client, m.download_category, m.arr_title, m.source_title,
            m.tmdb_id, m.tvdb_id,
            m.retry_count, m.next_retry_at, m.last_play_intent_at, m.play_intent_count, m.last_checked, m.last_rehydrated, m.last_pruned, m.last_error
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

// VisibilityWorkItems atomically claims rows that already re-added through Decypharr
// but are waiting for the CSI/rclone mount to expose the file path.
func (r *Repo) VisibilityWorkItems(ctx context.Context, limit int) ([]model.MediaCacheState, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.pool.Query(ctx, `
        WITH candidates AS (
            SELECT id
            FROM media_cache_state
            WHERE state = 'WAITING_FOR_VISIBILITY'
              AND rearm_requested = true
              AND (next_retry_at IS NULL OR next_retry_at <= now())
            ORDER BY updated_at ASC
            LIMIT $1
            FOR UPDATE SKIP LOCKED
        )
        UPDATE media_cache_state m
        SET last_checked = now()
        FROM candidates
        WHERE m.id = candidates.id
        RETURNING
            m.id::text, m.tenant, m.media_type, m.arr_id, m.symlink_path, m.state,
            m.rearm_requested, m.cached_until, m.torbox_torrent_id,
            m.infohash, m.magnet, m.download_client, m.download_category, m.arr_title, m.source_title,
            m.tmdb_id, m.tvdb_id,
            m.retry_count, m.next_retry_at, m.last_play_intent_at, m.play_intent_count, m.last_checked, m.last_rehydrated, m.last_pruned, m.last_error
    `, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (r *Repo) UpsertImportedMovie(ctx context.Context, tenant string, arrID int, title string, symlinkPath string, category string, cachedUntil time.Time, tmdbID int, tvdbID int) (model.MediaCacheState, error) {
	// If Seerr created a placeholder row before Radarr imported the movie, promote it
	// to the real Radarr ID instead of creating a second row. Placeholder rows use a
	// negative arr_id derived from TMDb because the historical schema requires arr_id.
	if tmdbID > 0 && arrID > 0 {
		row := r.pool.QueryRow(ctx, `
            UPDATE media_cache_state m
            SET arr_id = $2,
                symlink_path = $3,
                download_client = 'decypharr',
                download_category = NULLIF($5, ''),
                arr_title = COALESCE(NULLIF($6, ''), m.arr_title),
                tmdb_id = NULLIF($7, 0),
                tvdb_id = COALESCE(NULLIF($8, 0), m.tvdb_id),
                state = CASE
                    WHEN m.state IN ('REQUESTED', 'AVAILABLE', 'HOT', 'COOLING') THEN 'AVAILABLE'
                    ELSE m.state
                END,
                cached_until = CASE
                    WHEN m.state IN ('REQUESTED', 'AVAILABLE', 'HOT', 'COOLING')
                         AND m.cached_until IS NULL THEN $4
                    ELSE m.cached_until
                END,
                last_checked = now()
            WHERE m.tenant = $1
              AND m.media_type = 'movie'
              AND m.tmdb_id = $7
              AND m.arr_id < 0
              AND NOT EXISTS (
                  SELECT 1
                  FROM media_cache_state existing
                  WHERE existing.tenant = $1
                    AND existing.media_type = 'movie'
                    AND existing.arr_id = $2
              )
            RETURNING `+itemSelectColumns+`
        `, tenant, arrID, symlinkPath, cachedUntil, category, title, tmdbID, tvdbID)
		item, err := scanItem(row)
		if err == nil {
			return item, nil
		}
		if err != pgx.ErrNoRows {
			return model.MediaCacheState{}, err
		}
	}

	row := r.pool.QueryRow(ctx, `
        INSERT INTO media_cache_state (
            tenant, media_type, arr_id, symlink_path, state, rearm_requested,
            cached_until, download_client, download_category, arr_title, tmdb_id, tvdb_id,
            retry_count, next_retry_at, last_error, last_checked
        )
        VALUES ($1, 'movie', $2, $3, 'AVAILABLE', false, $4, 'decypharr', NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, 0), NULLIF($8, 0), 0, NULL, NULL, now())
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

func placeholderArrID(tmdbID int) int {
	if tmdbID < 0 {
		return tmdbID
	}
	return -tmdbID
}

func (r *Repo) UpsertRequestedMoviePlaceholder(ctx context.Context, tenant string, tmdbID int, title string, status string) (model.MediaCacheState, bool, error) {
	if tmdbID <= 0 {
		return model.MediaCacheState{}, false, pgx.ErrNoRows
	}
	if existing, found, err := r.GetStateByTMDB(ctx, tenant, model.MediaMovie, tmdbID); err != nil {
		return model.MediaCacheState{}, false, err
	} else if found {
		return existing, false, nil
	}
	arrID := placeholderArrID(tmdbID)
	row := r.pool.QueryRow(ctx, `
        INSERT INTO media_cache_state (
            tenant, media_type, arr_id, symlink_path, state, rearm_requested,
            cached_until, download_client, download_category, arr_title, tmdb_id,
            retry_count, next_retry_at, last_error, last_checked
        )
        VALUES ($1, 'movie', $2, '', 'REQUESTED', false, NULL, 'decypharr', 'radarr', NULLIF($3, ''), $4, 0, NULL, NULL, now())
        ON CONFLICT (tenant, media_type, arr_id)
        DO UPDATE SET
            arr_title = COALESCE(NULLIF($3, ''), media_cache_state.arr_title),
            tmdb_id = COALESCE(media_cache_state.tmdb_id, $4),
            last_checked = now()
        RETURNING `+itemSelectColumns+`
    `, tenant, arrID, title, tmdbID)
	item, err := scanItem(row)
	if err != nil {
		return model.MediaCacheState{}, false, err
	}
	return item, true, nil
}

func (r *Repo) UpsertRequestedRadarrMovie(ctx context.Context, tenant string, arrID int, title string, tmdbID int) (model.MediaCacheState, error) {
	if arrID <= 0 {
		return model.MediaCacheState{}, pgx.ErrNoRows
	}
	if tmdbID > 0 {
		row := r.pool.QueryRow(ctx, `
            UPDATE media_cache_state m
            SET arr_id = $2,
                arr_title = COALESCE(NULLIF($3, ''), m.arr_title),
                tmdb_id = $4,
                last_checked = now()
            WHERE m.tenant = $1
              AND m.media_type = 'movie'
              AND m.tmdb_id = $4
              AND m.arr_id < 0
              AND NOT EXISTS (
                  SELECT 1
                  FROM media_cache_state existing
                  WHERE existing.tenant = $1
                    AND existing.media_type = 'movie'
                    AND existing.arr_id = $2
              )
            RETURNING `+itemSelectColumns+`
        `, tenant, arrID, title, tmdbID)
		item, err := scanItem(row)
		if err == nil {
			return item, nil
		}
		if err != pgx.ErrNoRows {
			return model.MediaCacheState{}, err
		}
	}
	row := r.pool.QueryRow(ctx, `
        INSERT INTO media_cache_state (
            tenant, media_type, arr_id, symlink_path, state, rearm_requested,
            cached_until, download_client, download_category, arr_title, tmdb_id,
            retry_count, next_retry_at, last_error, last_checked
        ) VALUES (
            $1, 'movie', $2, '', 'REQUESTED', false,
            NULL, 'decypharr', 'radarr', NULLIF($3, ''), NULLIF($4, 0),
            0, NULL, NULL, now()
        )
        ON CONFLICT (tenant, media_type, arr_id)
        DO UPDATE SET
            arr_title = COALESCE(EXCLUDED.arr_title, media_cache_state.arr_title),
            tmdb_id = COALESCE(EXCLUDED.tmdb_id, media_cache_state.tmdb_id),
            last_checked = now()
        RETURNING `+itemSelectColumns+`
    `, tenant, arrID, title, tmdbID)
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
	MatchedArrID     *int
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
		if err != nil {
			return state, err
		}
		state.IsNew = true
	} else {
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
		if err != nil {
			return state, err
		}
		state.RearmRequestedAt = existingRearmedAt
	}

	matched, err := r.BackfillSeerrRequestArrID(ctx, req.Tenant, req.RequestKey)
	if err != nil {
		return state, err
	}
	state.MatchedArrID = matched
	return state, nil
}

func (r *Repo) BackfillSeerrRequestArrID(ctx context.Context, tenant string, requestKey string) (*int, error) {
	row := r.pool.QueryRow(ctx, `
        UPDATE media_cache_seerr_requests sr
        SET arr_id = m.arr_id,
            last_seen_at = now()
        FROM media_cache_state m
        WHERE sr.tenant = $1
          AND sr.request_key = $2
          AND sr.tenant = m.tenant
          AND sr.media_type = m.media_type
          AND sr.tmdb_id IS NOT NULL
          AND sr.tmdb_id = m.tmdb_id
          AND sr.arr_id IS NULL
        RETURNING sr.arr_id
    `, tenant, requestKey)
	var arrID int
	err := row.Scan(&arrID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &arrID, nil
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
            next_retry_at = NULL,
            last_error = NULL,
            last_checked = now()
        WHERE tenant = $1
          AND media_type = $2
          AND tmdb_id = $3
          AND arr_id > 0
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
            next_retry_at = NULL,
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
            next_retry_at = NULL,
            last_error = NULL,
            last_checked = now()
        WHERE tenant = $1
          AND media_type = $2
          AND arr_id = $3
        RETURNING `+itemSelectColumns+`
    `, tenant, mediaType, arrID)
	return scanItem(row)
}

func (r *Repo) RequestPrune(ctx context.Context, tenant string, mediaType model.MediaType, arrID int) (model.MediaCacheState, error) {
	row := r.pool.QueryRow(ctx, `
        UPDATE media_cache_state
        SET state = 'AVAILABLE',
            rearm_requested = false,
            cached_until = now() - interval '1 minute',
            retry_count = 0,
            next_retry_at = NULL,
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

func (r *Repo) GetState(ctx context.Context, tenant string, mediaType model.MediaType, arrID int) (model.MediaCacheState, bool, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT `+itemSelectColumns+`
        FROM media_cache_state
        WHERE tenant = $1 AND media_type = $2 AND arr_id = $3
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

func (r *Repo) GetStateByTMDB(ctx context.Context, tenant string, mediaType model.MediaType, tmdbID int) (model.MediaCacheState, bool, error) {
	if tmdbID <= 0 {
		return model.MediaCacheState{}, false, nil
	}
	row := r.pool.QueryRow(ctx, `
        SELECT `+itemSelectColumns+`
        FROM media_cache_state
        WHERE tenant = $1 AND media_type = $2 AND tmdb_id = $3
        ORDER BY updated_at DESC
        LIMIT 1
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

type PlaybackIntentUpsert struct {
	Tenant    string
	MediaType model.MediaType
	ArrID     int
	TMDBID    int
	Source    string
	Event     string
	Title     string
	User      string
	RawJSON   string
}

func (r *Repo) RecordUnmatchedPlaybackIntent(ctx context.Context, intent PlaybackIntentUpsert) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_playback_intents (
            tenant, media_type, arr_id, tmdb_id, source, event, title, username, raw,
            first_seen_at, last_seen_at, seen_count
        ) VALUES (
            $1, $2, NULLIF($3, 0), NULLIF($4, 0), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), COALESCE(NULLIF($9, '')::jsonb, '{}'::jsonb),
            now(), now(), 1
        )
        ON CONFLICT (tenant, media_type, source, event, tmdb_id, arr_id)
        DO UPDATE SET
            title = COALESCE(EXCLUDED.title, media_cache_playback_intents.title),
            username = COALESCE(EXCLUDED.username, media_cache_playback_intents.username),
            raw = EXCLUDED.raw,
            last_seen_at = now(),
            seen_count = media_cache_playback_intents.seen_count + 1
    `, intent.Tenant, intent.MediaType, intent.ArrID, intent.TMDBID, intent.Source, intent.Event, intent.Title, intent.User, intent.RawJSON)
	return err
}

type PlaybackIgnoredIntent struct {
	Tenant  string
	Source  string
	Event   string
	Title   string
	Reason  string
	RawJSON string
}

func (r *Repo) RecordIgnoredPlaybackIntent(ctx context.Context, intent PlaybackIgnoredIntent) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_playback_ignored (
            tenant, source, event, title, reason, raw, created_at
        ) VALUES (
            $1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), $5, COALESCE(NULLIF($6, '')::jsonb, '{}'::jsonb), now()
        )
    `, intent.Tenant, intent.Source, intent.Event, intent.Title, intent.Reason, intent.RawJSON)
	return err
}

func (r *Repo) MarkPlaybackIntentMatched(ctx context.Context, tenant string, mediaType model.MediaType, tmdbID int, arrID int, mediaID string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_playback_intents
        SET matched_media_id = $5,
            matched_at = now(),
            last_seen_at = now()
        WHERE tenant = $1
          AND media_type = $2
          AND (tmdb_id = NULLIF($3, 0) OR arr_id = NULLIF($4, 0))
          AND matched_media_id IS NULL
    `, tenant, mediaType, tmdbID, arrID, mediaID)
	return err
}

func (r *Repo) RecordPlaybackIntent(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET last_play_intent_at = now(),
            play_intent_count = play_intent_count + 1,
            last_checked = now()
        WHERE id = $1
    `, id)
	return err
}

func (r *Repo) MarkPlaybackRearmRequested(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'ARCHIVED',
            rearm_requested = true,
            cached_until = NULL,
            retry_count = 0,
            next_retry_at = NULL,
            last_error = NULL,
            last_checked = now()
        WHERE id = $1
          AND state IN ('REQUESTED', 'ARCHIVED', 'BROKEN', 'FAILED')
    `, id)
	return err
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

func (r *Repo) MarkWaitingVisibility(ctx context.Context, id string, msg string, nextRetryAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
        UPDATE media_cache_state
        SET state = 'WAITING_FOR_VISIBILITY',
            rearm_requested = true,
            next_retry_at = $3,
            last_checked = now(),
            last_error = NULLIF($2, '')
        WHERE id = $1
    `, id, msg, nextRetryAt)
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
            next_retry_at = NULL,
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
            next_retry_at = NULL,
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
            next_retry_at = CASE
                WHEN retry_count + 1 >= $3 THEN NULL
                ELSE now() + make_interval(secs => LEAST(3600, GREATEST(60, (POWER(2, retry_count)::int * 60))))
            END,
            last_checked = now(),
            last_error = $2
        WHERE id = $1
    `, id, msg, maxRetries)
	return err
}

func (r *Repo) SetProviderCooldown(ctx context.Context, tenant string, provider string, duration time.Duration, reason string) error {
	if duration <= 0 || provider == "" {
		return nil
	}
	seconds := int(duration.Seconds())
	if seconds <= 0 {
		seconds = 60
	}
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_provider_cooldowns (tenant, provider, cooldown_until, reason, updated_at)
        VALUES ($1, $2, now() + make_interval(secs => $3), NULLIF($4, ''), now())
        ON CONFLICT (tenant, provider)
        DO UPDATE SET cooldown_until = GREATEST(media_cache_provider_cooldowns.cooldown_until, EXCLUDED.cooldown_until),
                      reason = EXCLUDED.reason,
                      updated_at = now()
    `, tenant, provider, seconds, reason)
	return err
}

func (r *Repo) ProviderCooldownActive(ctx context.Context, tenant string, provider string) (bool, time.Time, string, error) {
	var until time.Time
	var reason *string
	err := r.pool.QueryRow(ctx, `
        SELECT cooldown_until, reason
        FROM media_cache_provider_cooldowns
        WHERE tenant = $1 AND provider = $2 AND cooldown_until > now()
    `, tenant, provider).Scan(&until, &reason)
	if err == pgx.ErrNoRows {
		return false, time.Time{}, "", nil
	}
	if err != nil {
		return false, time.Time{}, "", err
	}
	if reason == nil {
		return true, until, "", nil
	}
	return true, until, *reason, nil
}

func (r *Repo) Event(ctx context.Context, mediaID string, eventType string, metadata string) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_events (media_id, event_type, metadata)
        VALUES ($1, $2, $3::jsonb)
    `, mediaID, eventType, metadata)
	return err
}

func (r *Repo) RecordPlexRefresh(ctx context.Context, tenant string, mediaID string, arrID int, action string, scope string, path string, status string, message string) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO media_cache_plex_refreshes (
            tenant, media_id, arr_id, action, scope, path, status, message, created_at
        ) VALUES (
            $1, NULLIF($2, '')::uuid, NULLIF($3, 0), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, NULLIF($8, ''), now()
        )
    `, tenant, mediaID, arrID, action, scope, path, status, message)
	return err
}

func (r *Repo) StateSummary(ctx context.Context, tenant string) (map[string]int64, error) {
	out := map[string]int64{}
	rows, err := r.pool.Query(ctx, `
        SELECT state, count(*)
        FROM media_cache_state
        WHERE tenant = $1
        GROUP BY state
        ORDER BY state
    `, tenant)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return out, err
		}
		out[state] = count
	}
	return out, rows.Err()
}

func (r *Repo) ActiveProviderCooldowns(ctx context.Context, tenant string) ([]ProviderCooldown, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT tenant, provider, cooldown_until, COALESCE(reason, ''), updated_at
        FROM media_cache_provider_cooldowns
        WHERE tenant = $1 AND cooldown_until > now()
        ORDER BY cooldown_until DESC
    `, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderCooldown{}
	for rows.Next() {
		var c ProviderCooldown
		if err := rows.Scan(&c.Tenant, &c.Provider, &c.CooldownUntil, &c.Reason, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) RetryFailed(ctx context.Context, tenant string, limit int) (int64, error) {
	if limit <= 0 {
		limit = 10
	}
	cmd, err := r.pool.Exec(ctx, `
        WITH candidates AS (
            SELECT id
            FROM media_cache_state
            WHERE tenant = $1
              AND state IN ('BROKEN', 'FAILED', 'WAITING_FOR_VISIBILITY')
            ORDER BY updated_at ASC
            LIMIT $2
        )
        UPDATE media_cache_state m
        SET rearm_requested = CASE WHEN m.state IN ('BROKEN', 'FAILED') THEN true ELSE m.rearm_requested END,
            retry_count = 0,
            next_retry_at = NULL,
            last_error = NULL,
            updated_at = now()
        FROM candidates
        WHERE m.id = candidates.id
    `, tenant, limit)
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
}

func (r *Repo) PlexSelfHealWorkItems(ctx context.Context, tenant string, limit int, recentSeconds int) ([]model.MediaCacheState, error) {
	if limit <= 0 {
		limit = 5
	}
	if recentSeconds <= 0 {
		recentSeconds = 86400
	}
	rows, err := r.pool.Query(ctx, `
        SELECT `+itemSelectColumns+`
        FROM media_cache_state m
        WHERE m.tenant = $1
          AND m.media_type = 'movie'
          AND m.state = 'AVAILABLE'
          AND m.symlink_path <> ''
          AND (
              m.last_rehydrated IS NOT NULL
              OR m.last_play_intent_at IS NOT NULL
          )
          AND GREATEST(
              COALESCE(m.last_rehydrated, 'epoch'::timestamptz),
              COALESCE(m.last_play_intent_at, 'epoch'::timestamptz)
          ) > now() - ($3::int * interval '1 second')
          AND NOT EXISTS (
              SELECT 1
              FROM media_cache_plex_refreshes pr
              WHERE pr.tenant = m.tenant
                AND pr.media_id = m.id
                AND pr.status = 'success'
                AND pr.created_at >= GREATEST(
                    COALESCE(m.last_rehydrated, 'epoch'::timestamptz),
                    COALESCE(m.last_play_intent_at, 'epoch'::timestamptz)
                )
          )
        ORDER BY GREATEST(
            COALESCE(m.last_rehydrated, 'epoch'::timestamptz),
            COALESCE(m.last_play_intent_at, 'epoch'::timestamptz)
        ) DESC
        LIMIT $2
    `, tenant, limit, recentSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (r *Repo) Metrics(ctx context.Context, tenant string) (MetricsSnapshot, error) {
	s := MetricsSnapshot{Tenant: tenant, ItemsByState: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `
        SELECT state, count(*)
        FROM media_cache_state
        WHERE tenant = $1
        GROUP BY state
    `, tenant)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return s, err
		}
		s.ItemsByState[state] = count
	}
	if err := rows.Err(); err != nil {
		return s, err
	}

	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_seerr_requests WHERE tenant = $1`, tenant).Scan(&s.SeerrRequests)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_seerr_requests WHERE tenant = $1 AND rearm_requested_at IS NOT NULL`, tenant).Scan(&s.SeerrRearmed)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_events e JOIN media_cache_state m ON m.id = e.media_id WHERE m.tenant = $1`, tenant).Scan(&s.EventsTotal)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant = $1 AND state IN ('BROKEN','FAILED')`, tenant).Scan(&s.FailedItems)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant = $1 AND rearm_requested = true`, tenant).Scan(&s.RearmRequested)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant = $1 AND state = 'AVAILABLE' AND cached_until IS NOT NULL AND cached_until < now()`, tenant).Scan(&s.ExpiredPruneQueued)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant = $1 AND last_play_intent_at IS NOT NULL`, tenant).Scan(&s.PlaybackIntentRows)
	_ = r.pool.QueryRow(ctx, `SELECT COALESCE(sum(play_intent_count), 0) FROM media_cache_state WHERE tenant = $1`, tenant).Scan(&s.PlaybackIntentTotal)
	_ = r.pool.QueryRow(ctx, `SELECT COALESCE(sum(seen_count), 0) FROM media_cache_playback_intents WHERE tenant = $1`, tenant).Scan(&s.UnmatchedPlaybackTotal)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_playback_intents WHERE tenant = $1 AND matched_media_id IS NULL`, tenant).Scan(&s.UnmatchedPlaybackOpen)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_playback_ignored WHERE tenant = $1`, tenant).Scan(&s.PlaybackIgnoredTotal)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_state WHERE tenant = $1 AND state = 'WAITING_FOR_VISIBILITY'`, tenant).Scan(&s.WaitingVisibilityItems)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_provider_cooldowns WHERE tenant = $1 AND cooldown_until > now()`, tenant).Scan(&s.ProviderCooldownsActive)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_plex_refreshes WHERE tenant = $1`, tenant).Scan(&s.PlexRefreshTotal)
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM media_cache_plex_refreshes WHERE tenant = $1 AND status <> 'success'`, tenant).Scan(&s.PlexRefreshFailures)
	return s, nil
}
