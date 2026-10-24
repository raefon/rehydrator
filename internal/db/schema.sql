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
            'WAITING_FOR_VISIBILITY',
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
    tmdb_id INTEGER,
    tvdb_id INTEGER,

    retry_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,

    last_play_intent_at TIMESTAMPTZ,
    play_intent_count INT NOT NULL DEFAULT 0,

    last_checked TIMESTAMPTZ,
    last_rehydrated TIMESTAMPTZ,
    last_pruned TIMESTAMPTZ,
    last_error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant, media_type, arr_id)
);

-- Existing tables retain their old CHECK constraint after CREATE IF NOT EXISTS.
ALTER TABLE IF EXISTS media_cache_state
    DROP CONSTRAINT IF EXISTS media_cache_state_state_check;

ALTER TABLE IF EXISTS media_cache_state
    ADD CONSTRAINT media_cache_state_state_check CHECK (
        state IN (
            'REQUESTED',
            'AVAILABLE',
            'HOT',
            'COOLING',
            'ARCHIVED',
            'BROKEN',
            'REARMING',
            'WAITING_FOR_VISIBILITY',
            'PRUNING',
            'FAILED'
        )
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
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS tmdb_id INTEGER;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS tvdb_id INTEGER;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS last_play_intent_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS play_intent_count INT NOT NULL DEFAULT 0;

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
ON media_cache_state (rearm_requested, state, retry_count, next_retry_at, updated_at);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_prune_work
ON media_cache_state (state, cached_until);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_infohash
ON media_cache_state (infohash);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_tenant
ON media_cache_state (tenant);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_torbox_torrent_id
ON media_cache_state (torbox_torrent_id);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_tmdb
ON media_cache_state (tenant, media_type, tmdb_id);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_play_intent
ON media_cache_state (tenant, last_play_intent_at);

CREATE INDEX IF NOT EXISTS idx_media_cache_seerr_requests_seen
ON media_cache_seerr_requests (tenant, media_type, tmdb_id, last_seen_at);

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
-- v0.2.8: ignored playback events.
-- Jellyfin Cinema Trailers/pre-roll files emit their own media.play webhooks. Store a
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

-- v0.2.9: slow CSI visibility and provider cooldowns.
CREATE TABLE IF NOT EXISTS media_cache_provider_cooldowns (
    tenant TEXT NOT NULL,
    provider TEXT NOT NULL,
    cooldown_until TIMESTAMPTZ NOT NULL,
    reason TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant, provider)
);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_waiting_visibility
ON media_cache_state (tenant, state, next_retry_at)
WHERE state = 'WAITING_FOR_VISIBILITY';

CREATE INDEX IF NOT EXISTS idx_media_cache_provider_cooldowns_active
ON media_cache_provider_cooldowns (tenant, cooldown_until);

-- v0.3.0: Jellyfin library hygiene refresh audit.
CREATE TABLE IF NOT EXISTS media_cache_jellyfin_refreshes (
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

CREATE INDEX IF NOT EXISTS idx_media_cache_jellyfin_refreshes_tenant_created
ON media_cache_jellyfin_refreshes (tenant, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_cache_jellyfin_refreshes_media
ON media_cache_jellyfin_refreshes (tenant, media_id, created_at DESC);

-- v0.4.0: Sonarr episode-file support. Series rows use arr_id = episode_file_id.
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS series_id INTEGER,
    ADD COLUMN IF NOT EXISTS episode_id INTEGER,
    ADD COLUMN IF NOT EXISTS episode_file_id INTEGER,
    ADD COLUMN IF NOT EXISTS season_number INTEGER,
    ADD COLUMN IF NOT EXISTS episode_number INTEGER,
    ADD COLUMN IF NOT EXISTS series_title TEXT,
    ADD COLUMN IF NOT EXISTS episode_title TEXT,
    ADD COLUMN IF NOT EXISTS cache_group_key TEXT;

CREATE INDEX IF NOT EXISTS idx_media_cache_state_series_episode_file
ON media_cache_state (tenant, media_type, series_id, episode_file_id)
WHERE media_type = 'series';

CREATE INDEX IF NOT EXISTS idx_media_cache_state_path
ON media_cache_state (tenant, symlink_path)
WHERE symlink_path <> '';

CREATE INDEX IF NOT EXISTS idx_media_cache_state_cache_group
ON media_cache_state (tenant, cache_group_key)
WHERE cache_group_key IS NOT NULL AND cache_group_key <> '';

-- Persist original metainfo so rearm never needs an expiring indexer URL.
CREATE TABLE IF NOT EXISTS media_cache_torrent_sources (
 media_id UUID PRIMARY KEY REFERENCES media_cache_state(id) ON DELETE CASCADE,
 infohash TEXT NOT NULL,
 filename TEXT NOT NULL DEFAULT 'download.torrent',
 torrent_file BYTEA NOT NULL CHECK (octet_length(torrent_file) > 0 AND octet_length(torrent_file) <= 52428800),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Capture sources before import and retain exact download-to-import association.
CREATE TABLE IF NOT EXISTS media_cache_grab_sources (
 tenant TEXT NOT NULL,
 arr TEXT NOT NULL CHECK (arr IN ('radarr','sonarr')),
 owner_id INTEGER NOT NULL CHECK (owner_id>0),
 download_id TEXT NOT NULL,
 infohash TEXT NOT NULL DEFAULT '',
 magnet TEXT NOT NULL DEFAULT '',
 torrent_url TEXT NOT NULL DEFAULT '',
 source_title TEXT NOT NULL DEFAULT '',
 filename TEXT NOT NULL DEFAULT '',
 torrent_file BYTEA CHECK (octet_length(torrent_file)>0 AND octet_length(torrent_file)<=52428800),
 ready BOOLEAN NOT NULL DEFAULT false,
 imported_paths TEXT[] NOT NULL DEFAULT '{}',
 imported_at TIMESTAMPTZ,
 next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_error TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant,arr,owner_id,download_id)
);
CREATE INDEX IF NOT EXISTS idx_grab_source_pending ON media_cache_grab_sources(tenant,next_retry_at) WHERE NOT ready;
ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS source_download_id TEXT;
-- Submission source and release identity are independent. Never synthesize magnets.
ALTER TABLE media_cache_torrent_sources ALTER COLUMN torrent_file DROP NOT NULL;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS magnet TEXT NOT NULL DEFAULT '';
UPDATE media_cache_torrent_sources SET source_kind='torrent_file' WHERE source_kind IS NULL;
UPDATE media_cache_torrent_sources SET filename='download.torrent' WHERE source_kind='torrent_file' AND filename='';
ALTER TABLE media_cache_torrent_sources ALTER COLUMN source_kind SET DEFAULT 'recovery_required';
ALTER TABLE media_cache_torrent_sources ALTER COLUMN source_kind SET NOT NULL;

-- The old code generated bare xt-only magnets. Their original provenance cannot
-- be established. Preserve the legacy value on media_cache_state for audit, but
-- require source recovery instead of treating it as a proven magnet source.
INSERT INTO media_cache_torrent_sources(media_id,infohash,filename,torrent_file,source_kind,magnet)
SELECT id,infohash,'',NULL,
 CASE WHEN magnet ~* '^magnet:.*&' THEN 'magnet' ELSE 'recovery_required' END,
 CASE WHEN magnet ~* '^magnet:.*&' THEN magnet ELSE '' END
FROM media_cache_state WHERE infohash IS NOT NULL AND infohash<>''
ON CONFLICT(media_id) DO NOTHING;

ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
UPDATE media_cache_grab_sources SET source_kind=CASE
 WHEN NOT ready THEN 'unresolved'
 WHEN torrent_file IS NOT NULL THEN 'torrent_file'
 WHEN magnet ~* '^magnet:.*&' THEN 'magnet'
 ELSE 'recovery_required' END WHERE source_kind IS NULL;
UPDATE media_cache_grab_sources SET ready=false,last_error='source recovery required: legacy source provenance unknown'
 WHERE source_kind='recovery_required' AND ready;
UPDATE media_cache_grab_sources SET magnet='',torrent_url='' WHERE source_kind='torrent_file' AND ready;
UPDATE media_cache_grab_sources SET filename='download.torrent' WHERE source_kind='torrent_file' AND ready AND filename='';
UPDATE media_cache_grab_sources SET magnet='' WHERE source_kind='unresolved' AND torrent_url<>'';
UPDATE media_cache_state m SET magnet=NULL FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND s.source_kind='torrent_file' AND s.infohash=m.infohash;
ALTER TABLE media_cache_grab_sources ALTER COLUMN source_kind SET DEFAULT 'unresolved';
ALTER TABLE media_cache_grab_sources ALTER COLUMN source_kind SET NOT NULL;

DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_saved_source_payload' AND conrelid='media_cache_torrent_sources'::regclass) THEN
 ALTER TABLE media_cache_torrent_sources ADD CONSTRAINT ck_saved_source_payload CHECK (
 (source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND filename<>'') OR
 (source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND filename='') OR
 (source_kind='recovery_required' AND torrent_file IS NULL AND magnet='' AND filename=''));
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_grab_source_payload' AND conrelid='media_cache_grab_sources'::regclass) THEN
 ALTER TABLE media_cache_grab_sources ADD CONSTRAINT ck_grab_source_payload CHECK (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','torrent_file','magnet')) OR
 (ready AND source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND torrent_url='' AND filename<>'') OR
 (ready AND source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND torrent_url='' AND filename=''));
 END IF;
END $$;

-- NZB payloads, original Arr associations, and current opaque SAB job IDs.
ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS source_protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS download_job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS download_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS nzb_file BYTEA;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS nzb_filename TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS nzb_file BYTEA;
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS nzb_filename TEXT NOT NULL DEFAULT '';

ALTER TABLE media_cache_torrent_sources DROP CONSTRAINT IF EXISTS ck_saved_source_payload;
ALTER TABLE media_cache_torrent_sources ADD CONSTRAINT ck_saved_source_payload CHECK (
 (protocol='torrent' AND nzb_file IS NULL AND nzb_filename='' AND (
 (source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND filename<>'') OR
 (source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND filename='') OR
 (source_kind='recovery_required' AND torrent_file IS NULL AND magnet='' AND filename=''))) OR
 (protocol='usenet' AND source_kind='nzb_file' AND infohash='' AND torrent_file IS NULL AND filename='' AND magnet='' AND octet_length(nzb_file) BETWEEN 1 AND 52428800 AND nzb_file IS NOT NULL AND nzb_filename<>'' AND download_id<>''));
ALTER TABLE media_cache_grab_sources DROP CONSTRAINT IF EXISTS ck_grab_source_payload;
ALTER TABLE media_cache_grab_sources ADD CONSTRAINT ck_grab_source_payload CHECK (
 (protocol='torrent' AND nzb_file IS NULL AND nzb_filename='' AND (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','torrent_file','magnet')) OR
 (ready AND source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND torrent_url='' AND filename<>'') OR
 (ready AND source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND torrent_url='' AND filename=''))) OR
 (protocol='usenet' AND infohash='' AND torrent_file IS NULL AND filename='' AND magnet='' AND (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','nzb_file')) OR
 (ready AND source_kind='nzb_file' AND torrent_url='' AND octet_length(nzb_file) BETWEEN 1 AND 52428800 AND nzb_file IS NOT NULL AND nzb_filename<>''))));

-- One current runtime job per original source, shared by season-pack episodes.
CREATE TABLE IF NOT EXISTS media_cache_usenet_jobs (
 tenant TEXT NOT NULL, arr TEXT NOT NULL CHECK(arr IN ('radarr','sonarr')),
 owner_id INTEGER NOT NULL CHECK(owner_id>0), original_download_id TEXT NOT NULL,
 job_id TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant,arr,owner_id,original_download_id)
);

-- Persist observations independently of reconcile/prune timestamps.
CREATE TABLE IF NOT EXISTS media_cache_file_health (
 media_id UUID PRIMARY KEY REFERENCES media_cache_state(id) ON DELETE CASCADE,
 path TEXT NOT NULL, source_key TEXT NOT NULL,
 consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0),
 last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_healthy_at TIMESTAMPTZ, last_auto_rearm_at TIMESTAMPTZ,
 detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS media_cache_file_health_runs (
 tenant TEXT PRIMARY KEY, last_run_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS media_cache_provider_health (
 tenant TEXT NOT NULL, provider TEXT NOT NULL, infohash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('available','unavailable','unknown')),
 reason TEXT NOT NULL DEFAULT '', torrent_id BIGINT NOT NULL DEFAULT 0,
 checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 consecutive_failures INTEGER NOT NULL DEFAULT 0,
 last_auto_rearm_at TIMESTAMPTZ,
 PRIMARY KEY(tenant,provider,infohash)
);
