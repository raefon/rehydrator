-- v0.3.3 metadata guardrails
-- Normalize incomplete rows so they are treated as pending imports instead of retryable re-arm work.

UPDATE media_cache_state
SET state = 'REQUESTED',
    rearm_requested = false,
    retry_count = 0,
    next_retry_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE media_type = 'movie'
  AND state IN ('BROKEN', 'FAILED', 'REARMING', 'WAITING_FOR_VISIBILITY')
  AND COALESCE(BTRIM(symlink_path), '') = '';

UPDATE media_cache_state
SET rearm_requested = false,
    retry_count = 0,
    next_retry_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE media_type = 'movie'
  AND state = 'REQUESTED';
