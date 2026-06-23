-- v0.2.7: event-driven seed support.
-- Stores Plex/playback intents that arrive before Rehydrator has a tracked row.
CREATE TABLE IF NOT EXISTS media_cache_playback_intents (
    id BIGSERIAL PRIMARY KEY,
    tenant TEXT NOT NULL,
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
    arr_id INTEGER,
    tmdb_id INTEGER,
    source TEXT,
    event TEXT,
    title TEXT,
    username TEXT,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    seen_count INT NOT NULL DEFAULT 1,
    matched_media_id UUID REFERENCES media_cache_state(id) ON DELETE SET NULL,
    matched_at TIMESTAMPTZ,
    UNIQUE (tenant, media_type, source, event, tmdb_id, arr_id)
);

CREATE INDEX IF NOT EXISTS idx_media_cache_playback_intents_open
ON media_cache_playback_intents (tenant, media_type, tmdb_id, arr_id, matched_media_id, last_seen_at);

CREATE INDEX IF NOT EXISTS idx_media_cache_playback_intents_matched
ON media_cache_playback_intents (tenant, matched_at);
