CREATE TABLE IF NOT EXISTS media_cache_jellyfin_inventory (
 tenant TEXT NOT NULL,
 media_id UUID NOT NULL REFERENCES media_cache_state(id) ON DELETE CASCADE,
 path TEXT NOT NULL,
 last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant,media_id)
);
