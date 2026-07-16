-- v0.4.0: Sonarr episode-file support.
-- Rehydrator stores one series row per imported Sonarr episode file.
-- For media_type='series', arr_id is the Sonarr episodeFileId.

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
