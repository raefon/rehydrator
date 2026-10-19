-- Repair legacy state constraints that predate visibility waiting.
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

