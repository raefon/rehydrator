-- Persist original metainfo so rearm never needs an expiring indexer URL.
CREATE TABLE IF NOT EXISTS media_cache_torrent_sources (
 media_id UUID PRIMARY KEY REFERENCES media_cache_state(id) ON DELETE CASCADE,
 infohash TEXT NOT NULL,
 filename TEXT NOT NULL DEFAULT 'download.torrent',
 torrent_file BYTEA NOT NULL CHECK (octet_length(torrent_file) > 0 AND octet_length(torrent_file) <= 52428800),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
