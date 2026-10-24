package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const healthSourceKey = `m.source_protocol||':'||COALESCE(NULLIF(m.source_download_id,''),m.infohash,'')`

type FileHealthTarget struct {
	ID, Path, SourceKey string
	Failures            int
}
type FileHealthResult struct {
	Target   FileHealthTarget
	Readable bool
}
type FileHealthPass struct {
	tx     pgx.Tx
	tenant string
}

// AcquireFileHealthPass keeps a tenant lock for the bounded sweep. Durable run
// timestamps and observations prevent replica restarts from multiplying checks.
func (r *Repo) AcquireFileHealthPass(ctx context.Context, tenant string, interval time.Duration) (*FileHealthPass, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	close := func() { _ = tx.Rollback(ctx) }
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('file-health:'||$1,0))`, tenant).Scan(&locked); err != nil {
		close()
		return nil, err
	}
	if !locked {
		close()
		return nil, nil
	}
	var due bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM media_cache_file_health_runs WHERE tenant=$1 AND last_run_at>now()-($2*interval '1 second'))`, tenant, interval.Seconds()).Scan(&due)
	if err != nil {
		close()
		return nil, err
	}
	if !due {
		close()
		return nil, nil
	}
	return &FileHealthPass{tx: tx, tenant: tenant}, nil
}
func (p *FileHealthPass) Close(ctx context.Context) { _ = p.tx.Rollback(ctx) }
func (p *FileHealthPass) Targets(ctx context.Context, limit int) ([]FileHealthTarget, error) {
	rows, err := p.tx.Query(ctx, `SELECT m.id::text,m.symlink_path,`+healthSourceKey+`,CASE WHEN h.path=m.symlink_path AND h.source_key=`+healthSourceKey+` THEN h.consecutive_failures ELSE 0 END
 FROM media_cache_state m LEFT JOIN media_cache_file_health h ON h.media_id=m.id
 WHERE m.tenant=$1 AND m.state='AVAILABLE' AND NOT m.rearm_requested AND m.arr_id>0 AND m.symlink_path<>''
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND s.source_kind IN ('torrent_file','magnet','nzb_file') AND s.protocol=m.source_protocol AND (s.protocol='usenet' OR s.infohash=m.infohash))
 ORDER BY h.last_checked_at ASC NULLS FIRST,m.id LIMIT $2`, p.tenant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []FileHealthTarget
	for rows.Next() {
		var t FileHealthTarget
		if err := rows.Scan(&t.ID, &t.Path, &t.SourceKey, &t.Failures); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// Finish records a whole sample before queueing: a widespread outage suppresses
// every automatic rearm from that sample, including its first failed file.
func (p *FileHealthPass) Finish(ctx context.Context, results []FileHealthResult, outage bool, threshold, maxRearms int) (int, error) {
	queued := 0
	for _, result := range results {
		t := result.Target
		failures := 0
		detail := "readable"
		if !result.Readable && !outage {
			failures = min(t.Failures+1, threshold)
			detail = "file unreadable"
		}
		if outage {
			detail = "sample outage; recovery paused"
		}
		_, err := p.tx.Exec(ctx, `INSERT INTO media_cache_file_health(media_id,path,source_key,consecutive_failures,last_checked_at,last_healthy_at,detail)
 SELECT m.id,$2,$3,$4,now(),CASE WHEN $5 THEN now() ELSE NULL END,$6 FROM media_cache_state m WHERE m.id=$1 AND m.tenant=$7 AND m.symlink_path=$2 AND `+healthSourceKey+`=$3
 ON CONFLICT(media_id) DO UPDATE SET path=EXCLUDED.path,source_key=EXCLUDED.source_key,consecutive_failures=EXCLUDED.consecutive_failures,last_checked_at=now(),last_healthy_at=CASE WHEN $5 THEN now() ELSE media_cache_file_health.last_healthy_at END,detail=EXCLUDED.detail`, t.ID, t.Path, t.SourceKey, failures, result.Readable, detail, p.tenant)
		if err != nil {
			return 0, err
		}
		if outage || failures < threshold || queued >= maxRearms {
			continue
		}
		tag, err := p.tx.Exec(ctx, `UPDATE media_cache_state m SET state='BROKEN',rearm_requested=true,retry_count=0,next_retry_at=NULL,last_error='file health: repeated unreadable checks; automatic rearm queued',updated_at=now()
 WHERE m.id=$1 AND m.tenant=$2 AND m.state='AVAILABLE' AND NOT m.rearm_requested AND m.symlink_path=$3 AND `+healthSourceKey+`=$4
 AND NOT EXISTS(SELECT 1 FROM media_cache_file_health h WHERE h.media_id=m.id AND h.path=m.symlink_path AND h.source_key=`+healthSourceKey+` AND h.last_auto_rearm_at>now()-interval '24 hours')
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND s.protocol=m.source_protocol AND s.source_kind IN ('torrent_file','magnet','nzb_file'))`, t.ID, p.tenant, t.Path, t.SourceKey)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() > 0 {
			queued++
			if _, err = p.tx.Exec(ctx, `UPDATE media_cache_file_health SET last_auto_rearm_at=now(),consecutive_failures=0 WHERE media_id=$1`, t.ID); err != nil {
				return 0, err
			}
			if _, err = p.tx.Exec(ctx, `INSERT INTO media_cache_events(media_id,event_type,metadata) VALUES($1,'file_health_rearm_requested',$2::jsonb)`, t.ID, fmt.Sprintf(`{"failed_checks":%d}`, failures)); err != nil {
				return 0, err
			}
		}
	}
	if _, err := p.tx.Exec(ctx, `INSERT INTO media_cache_file_health_runs(tenant,last_run_at) VALUES($1,now()) ON CONFLICT(tenant) DO UPDATE SET last_run_at=now()`, p.tenant); err != nil {
		return 0, err
	}
	if err := p.tx.Commit(ctx); err != nil {
		return 0, err
	}
	return queued, nil
}
