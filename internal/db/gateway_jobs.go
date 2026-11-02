package db

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/raefon/rehydrator/internal/model"
	"github.com/raefon/rehydrator/internal/torbox"
	"strings"
	"time"
)

type GatewayJob struct {
	ReleaseFolder                                    string
	ProviderStatus                                   *torbox.ProviderStatus
	ID, Category, Filename, Name, State, LastError   string
	Protocol, SourceKind, InfoHash, Magnet, SourceID string
	TorrentFile                                      []byte
	NZB                                              []byte
	ProviderID                                       int64
	Attempts, MissingChecks                          int
	Hidden                                           bool
	CleanupRequested                                 bool
	CatalogRevision                                  int64
	CreatedAt                                        time.Time
	Files                                            []torbox.UsenetFile
}

const gatewayColumns = "id,category,filename,name,state,last_error,nzb,provider_id,attempts,missing_checks,hidden,created_at,files,protocol,source_kind,infohash,magnet,torrent_file,COALESCE(source_id,''),cleanup_requested,catalog_revision,provider_status,release_folder"

func scanGatewayJob(row pgx.Row) (GatewayJob, error) {
	var j GatewayJob
	var files, status []byte
	err := row.Scan(&j.ID, &j.Category, &j.Filename, &j.Name, &j.State, &j.LastError, &j.NZB, &j.ProviderID, &j.Attempts, &j.MissingChecks, &j.Hidden, &j.CreatedAt, &files, &j.Protocol, &j.SourceKind, &j.InfoHash, &j.Magnet, &j.TorrentFile, &j.SourceID, &j.CleanupRequested, &j.CatalogRevision, &status, &j.ReleaseFolder)
	if err == nil {
		err = json.Unmarshal(files, &j.Files)
		if err == nil && len(status) > 0 {
			err = json.Unmarshal(status, &j.ProviderStatus)
		}
	}
	return j, err
}
func (r *Repo) AddGatewayNZB(ctx context.Context, tenant, category, filename string, nzb []byte) (GatewayJob, error) {
	return r.AddGatewaySource(ctx, tenant, category, model.TorrentMetadata{Protocol: model.ProtocolUsenet, Kind: model.SourceNZBFile, NZBFile: nzb, NZBFilename: filename})
}
func (r *Repo) GatewayJobByID(ctx context.Context, tenant, id string) (GatewayJob, error) {
	return scanGatewayJob(r.pool.QueryRow(ctx, "SELECT "+gatewayColumns+" FROM gateway_jobs WHERE tenant=$1 AND id=$2", tenant, id))
}
func (r *Repo) GatewayJobs(ctx context.Context, tenant string) ([]GatewayJob, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+catalogColumns()+" FROM gateway_jobs WHERE tenant=$1 ORDER BY created_at,id", tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []GatewayJob
	for rows.Next() {
		j, err := scanGatewayJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ClaimGatewayJob keeps polling and submissions bounded across replicas. Persist
// Submitting before network I/O; a lost response never authorizes a blind retry.
func (r *Repo) ClaimGatewayJob(ctx context.Context, tenant string, submitInterval time.Duration, protocols ...string) (GatewayJob, bool, error) {
	protocol := ""
	if len(protocols) > 0 {
		protocol = protocols[0]
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return GatewayJob{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "cloud-nzb:"+tenant); err != nil {
		return GatewayJob{}, false, err
	}
	// Interrupted submissions are indeterminate, not safe to resubmit.
	if _, err = tx.Exec(ctx, `UPDATE gateway_jobs SET state='SubmissionUnknown',last_error='submission interrupted; inspect TorBox before retrying' WHERE tenant=$1 AND state='Submitting' AND next_check_at<now()`, tenant); err != nil {
		return GatewayJob{}, false, err
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_jobs SET state='Failed',last_error='cloud upload attempt limit reached' WHERE tenant=$1 AND state='Queued' AND attempts>=3", tenant); err != nil {
		return GatewayJob{}, false, err
	}
	j, err := scanGatewayJob(tx.QueryRow(ctx, "SELECT "+gatewayColumns+" FROM gateway_jobs WHERE tenant=$1 AND ($2='' OR protocol=$2) AND state NOT IN ('Failed','SubmissionUnknown','Submitting') AND next_check_at<=now() AND (state<>'Queued' OR NOT EXISTS(SELECT 1 FROM media_cache_cloud_usenet_gate g WHERE g.tenant=$1 AND g.next_submit_at>now())) ORDER BY next_check_at,id LIMIT 1 FOR UPDATE SKIP LOCKED", tenant, protocol))
	if err == pgx.ErrNoRows {
		return j, false, tx.Commit(ctx)
	}
	if err != nil {
		return j, false, err
	}
	if j.State == "Queued" {
		if j.Attempts >= 3 {
			return j, false, fmt.Errorf("cloud submission attempt limit reached")
		}
		if _, err = tx.Exec(ctx, "INSERT INTO media_cache_cloud_usenet_gate(tenant) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
			return j, false, err
		}
		tag, err := tx.Exec(ctx, "UPDATE media_cache_cloud_usenet_gate SET next_submit_at=now()+($2*interval '1 second') WHERE tenant=$1 AND next_submit_at<=now()", tenant, submitInterval.Seconds())
		if err != nil {
			return j, false, err
		}
		if tag.RowsAffected() == 0 {
			_, err = tx.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now()+interval '60 seconds' WHERE id=$1", j.ID)
			if err == nil {
				err = tx.Commit(ctx)
			}
			return j, false, err
		}
		j.Attempts++
		if _, err = tx.Exec(ctx, "UPDATE gateway_jobs SET state='Submitting',attempts=attempts+1,next_check_at=now()+interval '5 minutes' WHERE id=$1", j.ID); err != nil {
			return j, false, err
		}
	} else {
		if _, err = tx.Exec(ctx, "UPDATE gateway_jobs SET next_check_at=now()+interval '5 minutes' WHERE id=$1", j.ID); err != nil {
			return j, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return j, false, err
	}
	return j, true, nil
}
func (r *Repo) UpdateGatewayJob(ctx context.Context, tenant string, j GatewayJob, delay time.Duration) error {
	files, err := json.Marshal(j.Files)
	if err != nil {
		return err
	}
	if string(files) == "null" {
		files = []byte("[]")
	}
	_, err = r.pool.Exec(ctx, `UPDATE gateway_jobs SET provider_status=CASE WHEN provider_id IS DISTINCT FROM $3 THEN NULL ELSE provider_status END,catalog_revision=catalog_revision+CASE WHEN provider_id IS DISTINCT FROM $3 OR files IS DISTINCT FROM $5::jsonb OR (state IS DISTINCT FROM $4 AND NOT(state='Completed' AND $4='Mounting')) THEN 1 ELSE 0 END,provider_id=$3,state=CASE WHEN state='Completed' AND $4='Mounting' AND provider_id=$3 AND files=$5::jsonb THEN state ELSE $4 END,files=$5::jsonb,missing_checks=$6,last_error=$7,next_check_at=now()+($8*interval '1 second'),updated_at=now(),name=$9 WHERE tenant=$1 AND id=$2`, tenant, j.ID, j.ProviderID, j.State, string(files), j.MissingChecks, j.LastError, delay.Seconds(), j.Name)
	return err
}
func (r *Repo) HideGatewayJob(ctx context.Context, tenant, id string) error {
	_, err := r.pool.Exec(ctx, "UPDATE gateway_jobs SET hidden=true WHERE tenant=$1 AND id=$2", tenant, id)
	return err
}

func (r *Repo) GatewayJobCatalog(ctx context.Context, tenant, id string) (GatewayJob, error) {
	columns := catalogColumns()
	return scanGatewayJob(r.pool.QueryRow(ctx, "SELECT "+columns+" FROM gateway_jobs WHERE tenant=$1 AND id=$2", tenant, id))
}

func (r *Repo) GatewayRecoveryWaiting(ctx context.Context, tenant, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE media_cache_state m SET state='WAITING_FOR_VISIBILITY',rearm_requested=true,next_retry_at=now(),last_error='gateway: waiting for shared provider placement recovery'
 WHERE m.tenant=$1 AND ((m.source_protocol='usenet' AND m.source_download_id=$2) OR (m.source_protocol='torrent' AND m.infohash=(SELECT infohash FROM gateway_jobs WHERE tenant=$1 AND id=$2)))
 AND m.state IN ('AVAILABLE','BROKEN') AND NOT m.rearm_requested
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND ((s.protocol='usenet' AND s.source_kind='nzb_file' AND lower(s.download_id)=$2) OR (s.protocol='torrent' AND s.source_kind IN ('magnet','torrent_file') AND s.infohash=m.infohash)))`, tenant, id)
	return err
}

func catalogColumns() string {
	columns := strings.Replace(gatewayColumns, ",nzb,", ",NULL::bytea,", 1)
	columns = strings.Replace(columns, ",magnet,torrent_file,", ",''::text,NULL::bytea,", 1)
	return columns
}
