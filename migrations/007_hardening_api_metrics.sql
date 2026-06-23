ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_media_cache_state_rearm_work_v2
ON media_cache_state (rearm_requested, state, retry_count, next_retry_at, updated_at);

-- Backfill Seerr request audit rows with the matching Arr ID when TMDb was enough to match.
UPDATE media_cache_seerr_requests sr
SET arr_id = m.arr_id,
    last_seen_at = now()
FROM media_cache_state m
WHERE sr.tenant = m.tenant
  AND sr.media_type = m.media_type
  AND sr.tmdb_id IS NOT NULL
  AND sr.tmdb_id = m.tmdb_id
  AND sr.arr_id IS NULL;
