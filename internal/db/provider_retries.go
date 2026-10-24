package db

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// ClaimProviderRetry commits a shared release reservation before any network
// mutation. A crash consumes an attempt rather than immediately duplicating it.
func (r *Repo) ClaimProviderRetry(ctx context.Context, tenant, id, hash, path string) (bool, error) {
	var attempts int
	err := r.pool.QueryRow(ctx, `
 INSERT INTO media_cache_provider_retries AS retries(tenant,infohash)
 SELECT $1,$3 WHERE EXISTS(
 SELECT 1 FROM media_cache_state m JOIN media_cache_torrent_sources s ON s.media_id=m.id
 WHERE m.tenant=$1 AND m.id=$2 AND m.infohash=$3 AND m.symlink_path=$4
 AND m.state='WAITING_FOR_VISIBILITY' AND m.rearm_requested AND m.source_protocol='torrent'
 AND s.protocol='torrent' AND s.infohash=m.infohash AND s.source_kind IN ('magnet','torrent_file'))
 ON CONFLICT(tenant,infohash) DO UPDATE
 SET attempts=retries.attempts+1,
 next_attempt_at=now()+(interval '15 minutes' * power(2,retries.attempts)),
 last_error='retry reserved'
 WHERE retries.attempts<3 AND retries.next_attempt_at<=now()
 RETURNING attempts`, tenant, id, hash, path).Scan(&attempts)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return attempts > 0, err
}

func (r *Repo) RecordProviderRetry(ctx context.Context, tenant, hash, detail string) error {
	_, err := r.pool.Exec(ctx, "UPDATE media_cache_provider_retries SET last_error=$3 WHERE tenant=$1 AND infohash=$2", tenant, hash, detail)
	return err
}
