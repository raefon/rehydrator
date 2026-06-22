ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS tmdb_id INTEGER;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS tvdb_id INTEGER;

CREATE TABLE IF NOT EXISTS media_cache_seerr_requests (
    id BIGSERIAL PRIMARY KEY,
    tenant TEXT NOT NULL,
    request_key TEXT NOT NULL,
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
    tmdb_id INTEGER,
    tvdb_id INTEGER,
    arr_id INTEGER,
    title TEXT,
    status TEXT,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rearm_requested_at TIMESTAMPTZ,
    UNIQUE (tenant, request_key)
);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_tmdb
ON media_cache_state (tenant, media_type, tmdb_id);

CREATE INDEX IF NOT EXISTS idx_media_cache_seerr_requests_seen
ON media_cache_seerr_requests (tenant, media_type, tmdb_id, last_seen_at);
