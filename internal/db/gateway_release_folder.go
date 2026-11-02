package db

import "context"

// PinGatewayReleaseFolder fences stale verification work and fixes the import
// name once. Provider names may subsequently change without moving Arr's path.
func (r *Repo) PinGatewayReleaseFolder(ctx context.Context, tenant string, j GatewayJob, folder string) (GatewayJob, error) {
	return scanGatewayJob(r.pool.QueryRow(ctx, `UPDATE gateway_jobs SET release_folder=$4,state='Mounting',catalog_revision=catalog_revision+1 WHERE tenant=$1 AND id=$2 AND protocol='usenet' AND release_folder='' AND catalog_revision=$3 AND state IN ('Mounting','Completed') AND NOT cleanup_requested RETURNING `+gatewayColumns, tenant, j.ID, j.CatalogRevision, folder))
}
