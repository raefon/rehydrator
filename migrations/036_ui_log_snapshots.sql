-- Bounded sanitized snapshots shared by gateway and controller processes.
CREATE TABLE IF NOT EXISTS ui_log_snapshots (
 tenant TEXT NOT NULL,
 instance TEXT NOT NULL,
 source TEXT NOT NULL,
 entries JSONB NOT NULL DEFAULT '[]'::jsonb,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant, instance)
);
