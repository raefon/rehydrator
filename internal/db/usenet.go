package db

import (
	"context"
	"fmt"
	"strings"
)

// EnsureNZBJob serializes submissions for a season pack across episodes/pods.
// Original source identity remains independent of the provider's latest job ID.
func (r *Repo) EnsureNZBJob(ctx context.Context, id string, ensure func(string) (string, error)) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var tenant, arr, original string
	var owner int
	err = tx.QueryRow(ctx, `SELECT m.tenant,CASE WHEN m.media_type='movie' THEN 'radarr' ELSE 'sonarr' END,CASE WHEN m.media_type='movie' THEN m.arr_id ELSE m.series_id END,s.download_id FROM media_cache_state m JOIN media_cache_torrent_sources s ON s.media_id=m.id WHERE m.id=$1 AND s.protocol='usenet'`, id).Scan(&tenant, &arr, &owner, &original)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("nzb:%s:%s:%d:%s", tenant, arr, owner, original)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return "", err
	}
	var job string
	err = tx.QueryRow(ctx, `INSERT INTO media_cache_usenet_jobs(tenant,arr,owner_id,original_download_id,job_id) VALUES($1,$2,$3,$4,$4) ON CONFLICT(tenant,arr,owner_id,original_download_id) DO UPDATE SET updated_at=now() RETURNING job_id`, tenant, arr, owner, original).Scan(&job)
	if err != nil {
		return "", err
	}
	job, err = ensure(job)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(job) == "" {
		return "", fmt.Errorf("empty NZB job ID")
	}
	if _, err = tx.Exec(ctx, `UPDATE media_cache_usenet_jobs SET job_id=$5,updated_at=now() WHERE tenant=$1 AND arr=$2 AND owner_id=$3 AND original_download_id=$4`, tenant, arr, owner, original, job); err != nil {
		return "", err
	}
	// Checkpoint the submitted ID on all current imports of this exact source.
	if _, err = tx.Exec(ctx, `UPDATE media_cache_state m SET download_job_id=$5 WHERE tenant=$1 AND source_protocol='usenet' AND source_download_id=$4 AND ((media_type='movie' AND $2='radarr' AND arr_id=$3) OR (media_type='series' AND $2='sonarr' AND series_id=$3))`, tenant, arr, owner, original, job); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE media_cache_state SET state='WAITING_FOR_VISIBILITY',rearm_requested=true,next_retry_at=now()+interval '30 seconds',last_checked=now() WHERE id=$1`, id); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return job, nil
}
