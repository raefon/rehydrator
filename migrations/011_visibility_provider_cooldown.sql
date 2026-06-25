-- v0.2.9: slow CSI/rclone visibility and provider cooldown support.

ALTER TABLE IF EXISTS media_cache_state
    DROP CONSTRAINT IF EXISTS media_cache_state_state_check;

ALTER TABLE IF EXISTS media_cache_state
    ADD CONSTRAINT media_cache_state_state_check CHECK (
        state IN (
            'REQUESTED',
            'AVAILABLE',
            'HOT',
            'COOLING',
            'ARCHIVED',
            'BROKEN',
            'REARMING',
            'WAITING_FOR_VISIBILITY',
            'PRUNING',
            'FAILED'
        )
    );

CREATE TABLE IF NOT EXISTS media_cache_provider_cooldowns (
    tenant TEXT NOT NULL,
    provider TEXT NOT NULL,
    cooldown_until TIMESTAMPTZ NOT NULL,
    reason TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant, provider)
);

CREATE INDEX IF NOT EXISTS idx_media_cache_state_waiting_visibility
ON media_cache_state (tenant, state, next_retry_at)
WHERE state = 'WAITING_FOR_VISIBILITY';

CREATE INDEX IF NOT EXISTS idx_media_cache_provider_cooldowns_active
ON media_cache_provider_cooldowns (tenant, cooldown_until);
