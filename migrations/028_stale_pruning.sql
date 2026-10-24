ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS pruning_recovery_seen_at TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    IF NEW.state IS DISTINCT FROM OLD.state THEN
        NEW.pruning_recovery_seen_at = NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
