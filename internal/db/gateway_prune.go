package db

import (
	"context"
	"errors"
)

var (
	ErrGatewayPruneUnavailable = errors.New("job unavailable")
	ErrGatewayPruneActive      = errors.New("only failed jobs without pending cleanup can be deleted")
	ErrGatewayPruneInUse       = errors.New("job is linked to library media or shares a provider download")
)

// PruneFailedGatewayJob locks the state through provider deletion and removes
// only the job row. Retained sources and library records are never removed.
func (r *Repo) PruneFailedGatewayJob(ctx context.Context, tenant, id string, remove func(GatewayJob) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	j, err := scanGatewayJob(tx.QueryRow(ctx, "SELECT "+catalogColumns()+" FROM gateway_jobs WHERE tenant=$1 AND id=$2 FOR UPDATE", tenant, id))
	if err != nil {
		return ErrGatewayPruneUnavailable
	}
	if j.State != "Failed" || j.CleanupRequested {
		return ErrGatewayPruneActive
	}
	var used bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_cache_state WHERE tenant=$1 AND (source_download_id=$2 OR ($3='torrent' AND infohash=$4 AND $4<>''))) OR EXISTS(SELECT 1 FROM gateway_jobs WHERE protocol=$3 AND provider_id=$5 AND $5>0 AND NOT(tenant=$1 AND id=$2))`, tenant, id, j.Protocol, j.InfoHash, j.ProviderID).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return ErrGatewayPruneInUse
	}
	if err = remove(j); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM gateway_jobs WHERE tenant=$1 AND id=$2 AND state='Failed'", tenant, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
