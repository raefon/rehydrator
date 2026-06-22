-- v8: Decypharr add/re-arm, TorBox prune/delete by infohash.
ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS torbox_torrent_id TEXT;

ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS infohash TEXT;

CREATE INDEX IF NOT EXISTS idx_media_cache_state_infohash
ON media_cache_state (infohash);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_torbox_torrent_id
ON media_cache_state (torbox_torrent_id);
