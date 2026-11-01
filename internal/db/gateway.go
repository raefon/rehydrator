package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
)

// AddGatewaySource commits the original submission before acknowledging a client.
func (r *Repo) AddGatewaySource(ctx context.Context, tenant, category string, source model.TorrentMetadata) (CloudNZBJob, error) {
	s, err := source.ValidateSource()
	if err != nil {
		return CloudNZBJob{}, err
	}
	if s.Kind == model.SourceTorrentFile {
		if err = model.ValidateGatewayTorrent(s.TorrentFile); err != nil {
			return CloudNZBJob{}, err
		}
	}
	payload := s.NZBFile
	filename := s.NZBFilename
	if s.Kind == model.SourceTorrentFile {
		payload = s.TorrentFile
		filename = s.TorrentFilename
	}
	digest := sha256.Sum256(payload)
	if s.Kind == model.SourceMagnet {
		digest = sha256.Sum256([]byte(s.Magnet))
	}
	fingerprint := hex.EncodeToString(digest[:])
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CloudNZBJob{}, err
	}
	defer tx.Rollback(ctx)
	var sid string
	err = tx.QueryRow(ctx, `INSERT INTO gateway_sources(tenant,protocol,kind,fingerprint,filename,payload,magnet,infohash) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant,protocol,fingerprint) DO UPDATE SET fingerprint=EXCLUDED.fingerprint RETURNING id`, tenant, s.Protocol, string(s.Kind), fingerprint, filename, payload, s.Magnet, s.InfoHash).Scan(&sid)
	if err != nil {
		return CloudNZBJob{}, err
	}
	nzb := s.NZBFile
	if nzb == nil {
		nzb = []byte{}
	}
	var id string
	if s.Protocol == model.ProtocolTorrent {
		err = tx.QueryRow(ctx, `INSERT INTO gateway_jobs(id,tenant,category,fingerprint,filename,nzb,name,protocol,source_kind,infohash,magnet,torrent_file,source_id) VALUES('torrent-'||gen_random_uuid()::text,$1,$2,$3,$4,$5,$6,'torrent',$7,$8,$9,$10,$11) ON CONFLICT(tenant,infohash) WHERE protocol='torrent' DO UPDATE SET hidden=false,cleanup_requested=false RETURNING id`, tenant, category, "torrent:"+s.InfoHash, filename, nzb, s.SourceTitle, string(s.Kind), s.InfoHash, s.Magnet, s.TorrentFile, sid).Scan(&id)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO gateway_jobs(tenant,category,fingerprint,filename,nzb,name,source_id) VALUES($1,$2,$3,$4,$5,$4,$6) ON CONFLICT(tenant,category,fingerprint) DO UPDATE SET hidden=false,cleanup_requested=false RETURNING id`, tenant, category, fingerprint, filename, nzb, sid).Scan(&id)
	}
	if err != nil {
		return CloudNZBJob{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CloudNZBJob{}, err
	}
	return r.CloudNZB(ctx, tenant, id)
}
func (r *Repo) GatewayJob(ctx context.Context, tenant, id string) (CloudNZBJob, error) {
	return scanCloud(r.pool.QueryRow(ctx, "SELECT "+cloudColumns+" FROM gateway_jobs WHERE tenant=$1 AND (id=$2 OR (protocol='torrent' AND infohash=lower($2)))", tenant, id))
}
func (r *Repo) GatewayCategory(ctx context.Context, tenant, hash, category string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET category=$3 WHERE tenant=$1 AND infohash=$2 AND (category=$3 OR (state='Queued' AND attempts=0))`, tenant, hash, category)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("category cannot change after submission")
	}
	return err
}
func (r *Repo) GatewaySession(ctx context.Context, tenant, hash string, create bool) error {
	if create {
		if _, err := r.pool.Exec(ctx, `DELETE FROM gateway_sessions WHERE tenant=$1 AND expires_at<=now()`, tenant); err != nil {
			return err
		}
		_, err := r.pool.Exec(ctx, `INSERT INTO gateway_sessions(token_hash,tenant,expires_at) VALUES($1,$2,now()+interval '12 hours')`, hash, tenant)
		return err
	}
	var valid bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gateway_sessions WHERE token_hash=$1 AND tenant=$2 AND expires_at>now())`, hash, tenant).Scan(&valid)
	if err == nil && !valid {
		return fmt.Errorf("invalid session")
	}
	return err
}
func (r *Repo) GatewayLogout(ctx context.Context, tenant, hash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM gateway_sessions WHERE tenant=$1 AND token_hash=$2`, tenant, hash)
	return err
}
func (r *Repo) WakeGatewayJob(ctx context.Context, tenant, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET next_check_at=LEAST(next_check_at,now()) WHERE tenant=$1 AND id=$2 AND state NOT IN ('Failed','SubmissionUnknown','Submitting')`, tenant, id)
	return err
}

func (r *Repo) DeferGatewaySubmission(ctx context.Context, tenant, id, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE gateway_jobs SET state='Queued',attempts=GREATEST(attempts-1,0),last_error=$3,next_check_at=now()+interval '5 minutes',updated_at=now() WHERE tenant=$1 AND id=$2 AND state='Submitting'`, tenant, id, reason)
	return err
}
