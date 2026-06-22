CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS media_cache_state (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    tenant TEXT NOT NULL,

    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
    arr_id INTEGER NOT NULL,

    symlink_path TEXT NOT NULL,

    state TEXT NOT NULL CHECK (
        state IN (
            'REQUESTED',
            'AVAILABLE',
            'HOT',
            'COOLING',
            'ARCHIVED',
            'BROKEN',
            'REARMING',
            'PRUNING',
            'FAILED'
        )
    ) DEFAULT 'REQUESTED',

    rearm_requested BOOLEAN NOT NULL DEFAULT false,

    cached_until TIMESTAMPTZ,

    -- Legacy provider-specific field. Kept for compatibility with earlier rows.
    torbox_torrent_id TEXT,

    -- Decypharr/qBittorrent lifecycle identity. This is the preferred handle.
    infohash TEXT,
    magnet TEXT,
    download_client TEXT NOT NULL DEFAULT 'decypharr',
    download_category TEXT,
    arr_title TEXT,
    source_title TEXT,

    retry_count INT NOT NULL DEFAULT 0,

    last_checked TIMESTAMPTZ,
    last_rehydrated TIMESTAMPTZ,
    last_pruned TIMESTAMPTZ,
    last_error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant, media_type, arr_id)
);

-- Safe upgrade path for databases created by the TorBox-first prototype.
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS infohash TEXT;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS magnet TEXT;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS download_client TEXT NOT NULL DEFAULT 'decypharr';
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS download_category TEXT;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS arr_title TEXT;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS source_title TEXT;

CREATE TABLE IF NOT EXISTS media_cache_events (
    id BIGSERIAL PRIMARY KEY,
    media_id UUID REFERENCES media_cache_state(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_media_cache_state_updated_at ON media_cache_state;
CREATE TRIGGER trg_media_cache_state_updated_at
BEFORE UPDATE ON media_cache_state
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_media_cache_state_rearm_work
ON media_cache_state (rearm_requested, state, retry_count, updated_at);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_prune_work
ON media_cache_state (state, cached_until);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_infohash
ON media_cache_state (infohash);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_tenant
ON media_cache_state (tenant);
