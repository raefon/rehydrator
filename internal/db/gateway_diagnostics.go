package db

import (
	"context"
	"time"
)

type GatewayDiagnostics struct {
	NextCheckAt  time.Time             `json:"nextCheckAt"`
	VerifyNextAt time.Time             `json:"verifyNextAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	Library      []GatewayLibraryEntry `json:"library"`
}
type GatewayLibraryEntry struct {
	MediaType string `json:"mediaType"`
	ArrID     int    `json:"arrId"`
	State     string `json:"state"`
	Path      string `json:"path"`
}

func (r *Repo) GatewayDiagnostics(ctx context.Context, tenant string, j CloudNZBJob) (GatewayDiagnostics, error) {
	d := GatewayDiagnostics{Library: []GatewayLibraryEntry{}}
	err := r.pool.QueryRow(ctx, `SELECT next_check_at,verify_next_at,updated_at FROM gateway_jobs WHERE tenant=$1 AND id=$2`, tenant, j.ID).Scan(&d.NextCheckAt, &d.VerifyNextAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	rows, err := r.pool.Query(ctx, `SELECT media_type,arr_id,state,symlink_path FROM media_cache_state WHERE tenant=$1 AND source_protocol=$2 AND (($2='torrent' AND infohash=$3 AND $3<>'') OR ($2='usenet' AND source_download_id=$4)) ORDER BY media_type,arr_id LIMIT 500`, tenant, j.Protocol, j.InfoHash, j.ID)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry GatewayLibraryEntry
		if err = rows.Scan(&entry.MediaType, &entry.ArrID, &entry.State, &entry.Path); err != nil {
			return d, err
		}
		d.Library = append(d.Library, entry)
	}
	return d, rows.Err()
}

// RequestGatewayRecovery wakes normal reconciliation; it never resets attempts,
// fabricates absence, changes placement IDs, or directly authorizes an upload.
func (r *Repo) RequestGatewayRecovery(ctx context.Context, tenant string, j CloudNZBJob) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET next_check_at=LEAST(next_check_at,now()) WHERE tenant=$1 AND id=$2 AND state='Missing' AND missing_checks>=3 AND attempts<3 AND provider_id=$3 AND catalog_revision=$4 AND NOT cleanup_requested`, tenant, j.ID, j.ProviderID, j.CatalogRevision)
	return tag.RowsAffected() == 1, err
}
