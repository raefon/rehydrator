CREATE TABLE IF NOT EXISTS media_cache_provider_health (
 tenant TEXT NOT NULL, provider TEXT NOT NULL, infohash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('available','unavailable','unknown')),
 reason TEXT NOT NULL DEFAULT '', torrent_id BIGINT NOT NULL DEFAULT 0,
 checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 consecutive_failures INTEGER NOT NULL DEFAULT 0,
 last_auto_rearm_at TIMESTAMPTZ,
 PRIMARY KEY(tenant,provider,infohash)
);
