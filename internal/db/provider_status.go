package db

import (
	"context"
	"encoding/json"
	"github.com/raefon/rehydrator/internal/torbox"
)

func (r *Repo) RecordProviderStatus(ctx context.Context, tenant string, j GatewayJob, status torbox.ProviderStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE gateway_jobs SET provider_status=$4::jsonb WHERE tenant=$1 AND id=$2 AND provider_id=$3`, tenant, j.ID, j.ProviderID, string(data))
	return err
}
func (r *Repo) RecordProviderCheckFailure(ctx context.Context, tenant string, j GatewayJob) error {
	_, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET provider_status=provider_status || jsonb_build_object('checkFailed',true,'lastAttemptAt',now()) WHERE tenant=$1 AND id=$2 AND provider_id=$3 AND provider_status IS NOT NULL`, tenant, j.ID, j.ProviderID)
	return err
}
