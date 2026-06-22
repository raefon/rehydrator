-- Upgrade migration for the Decypharr-first lifecycle design.
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

CREATE INDEX IF NOT EXISTS idx_media_cache_state_infohash
ON media_cache_state (infohash);
