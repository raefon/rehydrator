package db

import "context"

// ReleaseArchivedForHealth requires current Arr import ownership and validated
// saved source bytes/magnet to be checked by the caller. It queues no provider
// mutation; the normal health sweep applies confirmation and recovery caps.
func (r *Repo) ReleaseArchivedForHealth(ctx context.Context, tenant, id, path, hash string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `WITH restored AS (
 UPDATE media_cache_state m SET state='BROKEN',rearm_requested=false,
 retry_count=0,next_retry_at=NULL,last_checked=now(),
 last_error='archived recovery enabled: awaiting provider health confirmation'
 WHERE m.tenant=$1 AND m.id=$2 AND m.symlink_path=$3 AND m.infohash=$4
 AND m.source_protocol='torrent' AND m.state='ARCHIVED' AND NOT m.rearm_requested
 AND m.arr_id>0 AND m.symlink_path<>''
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id
 AND s.protocol='torrent' AND s.infohash=m.infohash AND s.source_kind IN ('magnet','torrent_file'))
 RETURNING m.id,m.infohash
 )
 INSERT INTO media_cache_events(media_id,event_type,metadata)
 SELECT id,'archived_recovery_enabled',jsonb_build_object('infohash',infohash,'source','arr_inventory') FROM restored`,
		tenant, id, path, hash)
	return tag.RowsAffected() > 0, err
}
