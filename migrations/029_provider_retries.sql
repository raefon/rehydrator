CREATE TABLE IF NOT EXISTS media_cache_provider_retries (
 tenant TEXT NOT NULL,
 infohash TEXT NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '15 minutes',
 last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant,infohash)
);
