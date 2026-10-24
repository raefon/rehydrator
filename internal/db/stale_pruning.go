package db

import (
	"context"
	"time"
)

// ReleaseStalePruning requires current Arr ownership to be checked by its caller.
// Never claim availability or request a provider mutation here.
func (r *Repo) ReleaseStalePruning(ctx context.Context, tenant, id, path string, staleAfter time.Duration) (bool, error) {
	// This marker survives routine inventory updates, unlike updated_at.
	_, err := r.pool.Exec(ctx, `
 UPDATE media_cache_state SET pruning_recovery_seen_at=now()
 WHERE tenant=$1 AND id=$2 AND symlink_path=$3 AND state='PRUNING'
 AND NOT rearm_requested AND pruning_recovery_seen_at IS NULL
 `, tenant, id, path)
	if err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx, `
 UPDATE media_cache_state m
 SET state='BROKEN',rearm_requested=false,retry_count=0,next_retry_at=NULL,
 last_error='pruning disabled: stale operation returned to provider health checks',pruning_recovery_seen_at=NULL,updated_at=now()
 WHERE m.tenant=$1 AND m.id=$2 AND m.symlink_path=$3 AND m.symlink_path<>''
 AND m.arr_id>0 AND m.state='PRUNING' AND NOT m.rearm_requested
 AND m.pruning_recovery_seen_at<now()-($4*interval '1 second') AND m.source_protocol='torrent'
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id
 AND s.protocol='torrent' AND s.infohash=m.infohash AND s.source_kind IN ('torrent_file','magnet'))
 `, tenant, id, path, staleAfter.Seconds())
	return tag.RowsAffected() > 0, err
}
