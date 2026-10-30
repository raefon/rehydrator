package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/raefon/rehydrator/internal/model"
)

// RequestSeerrTV is one-shot across webhook and polling. Approval and season
// scope are checked before entry; IDs below are TVDB IDs, never episode-file IDs.
func (r *Repo) RequestSeerrTV(ctx context.Context, tenant, key string, tvdbID int, seasons []int) (int, error) {
	if tvdbID <= 0 || len(seasons) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("seerr-tv:%s:%d", tenant, tvdbID)); err != nil {
		return 0, err
	}
	var done bool
	err = tx.QueryRow(ctx, "SELECT rearm_requested_at IS NOT NULL FROM media_cache_seerr_requests WHERE tenant=$1 AND request_key=$2", tenant, key).Scan(&done)
	if err != nil || done {
		return 0, err
	}
	seasonIDs := make([]int32, len(seasons))
	for i, n := range seasons {
		seasonIDs[i] = int32(n)
	}
	rows, err := tx.Query(ctx, `WITH candidates AS (
 SELECT m.id,row_number() OVER(PARTITION BY m.source_protocol,CASE WHEN m.source_protocol='torrent' THEN m.infohash ELSE COALESCE(NULLIF(m.source_download_id,''),m.id::text) END ORDER BY m.id) AS position
 FROM media_cache_state m WHERE m.tenant=$1 AND m.media_type='series' AND m.tvdb_id=$2 AND m.season_number=ANY($3)
 AND m.state IN ('ARCHIVED','BROKEN','FAILED') AND NOT m.rearm_requested
 AND m.arr_id>0 AND COALESCE(m.symlink_path,'')<>''
 AND EXISTS(SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id
 AND ((s.protocol='torrent' AND m.source_protocol='torrent' AND s.infohash=m.infohash AND s.source_kind IN ('magnet','torrent_file'))
 OR (s.protocol='usenet' AND m.source_protocol='usenet' AND s.source_kind='nzb_file')))
 )
 UPDATE media_cache_state m SET state=CASE WHEN candidates.position=1 AND NOT EXISTS(
 SELECT 1 FROM media_cache_state active WHERE active.tenant=m.tenant AND active.media_type='series'
 AND active.source_protocol=m.source_protocol AND active.rearm_requested
 AND active.state IN ('ARCHIVED','REARMING','WAITING_FOR_VISIBILITY')
 AND ((m.source_protocol='torrent' AND active.infohash=m.infohash)
 OR (m.source_protocol='usenet' AND active.series_id=m.series_id AND active.source_download_id=m.source_download_id AND m.source_download_id<>''))
 ) THEN 'ARCHIVED' ELSE 'WAITING_FOR_VISIBILITY' END,rearm_requested=true,
 cached_until=NULL,retry_count=0,next_retry_at=NULL,last_error=NULL,last_checked=now()
 FROM candidates WHERE m.id=candidates.id AND m.state IN ('ARCHIVED','BROKEN','FAILED') AND NOT m.rearm_requested RETURNING m.id::text`, tenant, tvdbID, seasonIDs)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	payload, _ := json.Marshal(map[string]any{"request_key": key, "tvdb_id": tvdbID, "seasons": seasons, "source": "seerr_tv"})
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO media_cache_events(media_id,event_type,metadata) VALUES($1,'seerr_tv_rearm_requested',$2::jsonb)`, id, string(payload)); err != nil {
			return 0, err
		}
	}
	// Also consume unmatched requests: acquisition remains with Seerr/Sonarr and
	// a persistent old request must not unexpectedly rearm later imports.
	if _, err = tx.Exec(ctx, "UPDATE media_cache_seerr_requests SET rearm_requested_at=now() WHERE tenant=$1 AND request_key=$2", tenant, key); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// RecordSeerrTV keeps series-level request identity out of episode-file arr_id.
func (r *Repo) RecordSeerrTV(ctx context.Context, tenant string, requestID, tmdbID int, title, status, raw string) (string, error) {
	key := fmt.Sprintf("request:%d", requestID)
	if requestID <= 0 {
		return "", fmt.Errorf("TV requests require a stable Seerr request ID")
	}
	_, err := r.UpsertSeerrRequest(ctx, SeerrRequestUpsert{Tenant: tenant, RequestKey: key, MediaType: model.MediaSeries, TMDBID: tmdbID, Title: title, Status: status, RawJSON: raw})
	return key, err
}
