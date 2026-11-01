-- Optional observations collected by the existing provider worker.
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS provider_status JSONB;
