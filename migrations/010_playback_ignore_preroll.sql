-- v0.2.8: ignored playback events.
-- Plex Cinema Trailers/pre-roll files emit their own media.play webhooks. Store a
-- lightweight audit row and skip unmatched playback storage/Radarr refresh.
CREATE TABLE IF NOT EXISTS media_cache_playback_ignored (
    id BIGSERIAL PRIMARY KEY,
    tenant TEXT NOT NULL,
    source TEXT,
    event TEXT,
    title TEXT,
    reason TEXT NOT NULL,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_media_cache_playback_ignored_tenant_created
ON media_cache_playback_ignored (tenant, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_cache_playback_ignored_reason
ON media_cache_playback_ignored (tenant, reason, created_at DESC);
