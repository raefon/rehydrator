-- Persist observations independently of reconcile/prune timestamps.
CREATE TABLE IF NOT EXISTS media_cache_file_health (
 media_id UUID PRIMARY KEY REFERENCES media_cache_state(id) ON DELETE CASCADE,
 path TEXT NOT NULL, source_key TEXT NOT NULL,
 consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0),
 last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_healthy_at TIMESTAMPTZ, last_auto_rearm_at TIMESTAMPTZ,
 detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS media_cache_file_health_runs (
 tenant TEXT PRIMARY KEY, last_run_at TIMESTAMPTZ NOT NULL
);
