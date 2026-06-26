-- v0.3.0: Plex library hygiene refresh audit.
CREATE TABLE IF NOT EXISTS media_cache_plex_refreshes (
    id BIGSERIAL PRIMARY KEY,
    tenant TEXT NOT NULL,
    media_id UUID REFERENCES media_cache_state(id) ON DELETE SET NULL,
    arr_id INTEGER,
    action TEXT,
    scope TEXT,
    path TEXT,
    status TEXT NOT NULL CHECK (status IN ('success', 'failed', 'skipped')),
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_media_cache_plex_refreshes_tenant_created
ON media_cache_plex_refreshes (tenant, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_cache_plex_refreshes_media
ON media_cache_plex_refreshes (tenant, media_id, created_at DESC);
