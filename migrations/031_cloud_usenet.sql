CREATE TABLE IF NOT EXISTS media_cache_cloud_usenet (
 id TEXT PRIMARY KEY DEFAULT ('cloud-nzb-'||gen_random_uuid()::text),
 tenant TEXT NOT NULL,
 category TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 filename TEXT NOT NULL,
 nzb BYTEA NOT NULL,
 name TEXT NOT NULL,
 provider_id BIGINT NOT NULL DEFAULT 0,
 state TEXT NOT NULL DEFAULT 'Queued' CHECK(state IN ('Queued','Submitting','Downloading','Mounting','Completed','Missing','Failed','SubmissionUnknown')),
 files JSONB NOT NULL DEFAULT '[]'::jsonb,
 attempts INTEGER NOT NULL DEFAULT 0,
 missing_checks INTEGER NOT NULL DEFAULT 0,
 hidden BOOLEAN NOT NULL DEFAULT false,
 next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant,category,fingerprint)
);
CREATE TABLE IF NOT EXISTS media_cache_cloud_usenet_gate (
 tenant TEXT PRIMARY KEY,
 next_submit_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cloud_usenet_due ON media_cache_cloud_usenet(tenant,next_check_at);
