package db

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// ClaimGatewayMount uses a separate lease so provider polling cannot starve
// mount verification, and two controller replicas cannot claim the same work.
func (r *Repo) ClaimGatewayMount(ctx context.Context, tenant string) (GatewayJob, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return GatewayJob{}, false, err
	}
	defer tx.Rollback(ctx)
	j, err := scanGatewayJob(tx.QueryRow(ctx, "SELECT "+catalogColumns()+" FROM gateway_jobs WHERE tenant=$1 AND (state='Mounting' OR cleanup_requested OR (protocol='usenet' AND state='Completed' AND NOT hidden AND release_folder='')) AND verify_next_at<=now() ORDER BY verify_next_at,id LIMIT 1 FOR UPDATE SKIP LOCKED", tenant))
	if err == pgx.ErrNoRows {
		return j, false, tx.Commit(ctx)
	}
	if err != nil {
		return j, false, err
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_jobs SET verify_next_at=now()+interval '1 minute' WHERE tenant=$1 AND id=$2", tenant, j.ID); err != nil {
		return j, false, err
	}
	return j, true, tx.Commit(ctx)
}

// FinishGatewayMount cannot complete a stale placement/catalog after provider
// loss or replacement. Observation and verification never overwrite each other.
func (r *Repo) FinishGatewayMount(ctx context.Context, tenant string, j GatewayJob, detail string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET state=CASE WHEN $4='' THEN 'Completed' ELSE state END,last_error=$4,updated_at=now() WHERE tenant=$1 AND id=$2 AND state='Mounting' AND catalog_revision=$3 AND NOT cleanup_requested`, tenant, j.ID, j.CatalogRevision, detail)
	return tag.RowsAffected() > 0 && detail == "", err
}
func (r *Repo) RequestGatewayCleanup(ctx context.Context, tenant, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET hidden=true,cleanup_requested=true,verify_next_at=now() WHERE tenant=$1 AND id=$2`, tenant, id)
	return err
}
func (r *Repo) FinishGatewayCleanup(ctx context.Context, tenant string, j GatewayJob) error {
	_, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET cleanup_requested=false WHERE tenant=$1 AND id=$2 AND catalog_revision=$3 AND hidden`, tenant, j.ID, j.CatalogRevision)
	return err
}

func (r *Repo) GatewayCatalogReady(ctx context.Context, tenant string) error {
	rows, err := r.pool.Query(ctx, `SELECT id,catalog_revision,verify_next_at,cleanup_requested,release_folder FROM gateway_jobs WHERE tenant=$1 LIMIT 0`, tenant)
	if err != nil {
		return err
	}
	rows.Close()
	return rows.Err()
}
