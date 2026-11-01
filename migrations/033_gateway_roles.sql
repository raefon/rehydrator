-- Independent mount verification lease; the gateway never waits for CSI.
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS verify_next_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS catalog_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS cleanup_requested BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS gateway_verification_due ON gateway_jobs(tenant,verify_next_at) WHERE state='Mounting' OR cleanup_requested;
