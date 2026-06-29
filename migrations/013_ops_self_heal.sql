-- v0.3.2: operational endpoints + v0.3.3 self-healing worker support.
-- No destructive changes. These indexes make status/repair lookups cheap.

CREATE INDEX IF NOT EXISTS idx_media_cache_state_available_recent_rehydrated
ON media_cache_state (tenant, media_type, state, last_rehydrated DESC)
WHERE state = 'AVAILABLE' AND last_rehydrated IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_media_cache_state_available_recent_play
ON media_cache_state (tenant, media_type, state, last_play_intent_at DESC)
WHERE state = 'AVAILABLE' AND last_play_intent_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_media_cache_plex_refreshes_success_media
ON media_cache_plex_refreshes (tenant, media_id, created_at DESC)
WHERE status = 'success';
