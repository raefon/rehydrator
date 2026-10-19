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
