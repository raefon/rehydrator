package db

import (
	"context"
	"github.com/raefon/rehydrator/internal/model"
	"time"
)

// ImportedInventory uses the synchronized Arr inventory projection. No source or
// recent-play requirement: old, never-played imports also need indexing.
func (r *Repo) ImportedInventory(ctx context.Context, tenant string) ([]model.MediaCacheState, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+itemSelectColumns+` FROM media_cache_state m
 WHERE tenant=$1 AND state='AVAILABLE' AND NOT rearm_requested AND arr_id>0
 AND media_type IN ('movie','series') AND COALESCE(symlink_path,'')<>''
 ORDER BY (SELECT last_attempt_at FROM media_cache_jellyfin_inventory i WHERE i.tenant=m.tenant AND i.media_id=m.id) ASC NULLS FIRST,m.id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}
func (r *Repo) ClaimJellyfinInventory(ctx context.Context, tenant, id, path string, cooldown time.Duration) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO media_cache_jellyfin_inventory AS i(tenant,media_id,path)
 SELECT $1,m.id,$3 FROM media_cache_state m WHERE m.tenant=$1 AND m.id=$2 AND m.state='AVAILABLE' AND NOT m.rearm_requested
 ON CONFLICT(tenant,media_id) DO UPDATE SET path=EXCLUDED.path,last_attempt_at=now()
 WHERE i.path<>EXCLUDED.path OR i.last_attempt_at<=now()-($4*interval '1 second')`, tenant, id, path, cooldown.Seconds())
	return tag.RowsAffected() > 0, err
}

// FlagMissingInventorySource changes only the same still-idle torrent import.
// Concurrent source capture or lifecycle changes must win over this observation.
func (r *Repo) FlagMissingInventorySource(ctx context.Context, tenant, id, path string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE media_cache_state m
 SET state='FAILED',rearm_requested=false,next_retry_at=NULL,last_checked=now(),
 last_error='source recovery required: torrent library path is missing and no matching saved torrent file or magnet is available'
 WHERE m.tenant=$1 AND m.id=$2 AND m.symlink_path=$3 AND m.state='AVAILABLE'
 AND NOT m.rearm_requested AND m.source_protocol='torrent'
 AND NOT EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id
 AND s.protocol='torrent' AND s.source_kind IN ('magnet','torrent_file') AND s.infohash=m.infohash)`,
		tenant, id, path)
	return tag.RowsAffected() > 0, err
}
