ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS last_play_intent_at TIMESTAMPTZ;

ALTER TABLE IF EXISTS media_cache_state
    ADD COLUMN IF NOT EXISTS play_intent_count INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_media_cache_state_play_intent
ON media_cache_state (tenant, last_play_intent_at);
