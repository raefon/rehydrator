-- Arr inventory reconciliation tombstones. Safe to run repeatedly.
CREATE TABLE IF NOT EXISTS media_cache_tombstones (
    id BIGSERIAL PRIMARY KEY,
    tenant TEXT NOT NULL,
    arr_app TEXT NOT NULL CHECK (arr_app IN ('radarr', 'sonarr')),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
    arr_id INTEGER NOT NULL,
    external_id INTEGER,
    title TEXT,
    reason TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_media_cache_tombstones_tenant_deleted
    ON media_cache_tombstones (tenant, deleted_at DESC);
