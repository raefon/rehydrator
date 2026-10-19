package db

import (
	"context"
	"strings"

	"github.com/raefon/rehydrator/internal/model"
)

func (r *Repo) QueueGrabSource(ctx context.Context, g model.GrabSource) (bool, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO media_cache_grab_sources(tenant,arr,owner_id,download_id,infohash,magnet,torrent_url,source_title,source_kind)
 VALUES($1,$2,$3,lower($4),$5,$6,$7,$8,$9)
 ON CONFLICT(tenant,arr,owner_id,download_id) DO UPDATE SET
 torrent_url=CASE WHEN EXCLUDED.magnet<>'' THEN '' ELSE COALESCE(NULLIF(EXCLUDED.torrent_url,''),media_cache_grab_sources.torrent_url) END,
 magnet=CASE WHEN EXCLUDED.torrent_url<>'' THEN '' ELSE COALESCE(NULLIF(EXCLUDED.magnet,''),media_cache_grab_sources.magnet) END,
 source_kind=CASE WHEN EXCLUDED.torrent_url<>'' OR EXCLUDED.magnet<>'' THEN 'unresolved' ELSE media_cache_grab_sources.source_kind END,
 infohash=COALESCE(NULLIF(EXCLUDED.infohash,''),media_cache_grab_sources.infohash),
 source_title=COALESCE(NULLIF(EXCLUDED.source_title,''),media_cache_grab_sources.source_title)
 WHERE NOT media_cache_grab_sources.ready`, g.Tenant, g.Arr, g.OwnerID, g.DownloadID, g.Torrent.InfoHash, g.Torrent.Magnet, g.Torrent.TorrentURL, g.Torrent.SourceTitle, model.SourceUnresolved)
	return result.RowsAffected() > 0, err
}

func (r *Repo) CompleteGrabSource(ctx context.Context, g model.GrabSource) error {
	t, err := g.Torrent.ValidateSource()
	if err != nil {
		return err
	}
	g.Torrent = t
	_, err = r.pool.Exec(ctx, `UPDATE media_cache_grab_sources SET infohash=$5,magnet=$6,torrent_url='',source_title=$7,filename=$8,torrent_file=$9,source_kind=$10,ready=true,last_error=NULL
 WHERE tenant=$1 AND arr=$2 AND owner_id=$3 AND download_id=lower($4) AND NOT ready`, g.Tenant, g.Arr, g.OwnerID, g.DownloadID, g.Torrent.InfoHash, g.Torrent.Magnet, g.Torrent.SourceTitle, g.Torrent.TorrentFilename, g.Torrent.TorrentFile, g.Torrent.Kind)
	return err
}

func (r *Repo) RetryGrabSource(ctx context.Context, g model.GrabSource, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE media_cache_grab_sources SET next_retry_at=now()+interval '1 minute',last_error=$5
 WHERE tenant=$1 AND arr=$2 AND owner_id=$3 AND download_id=lower($4) AND NOT ready`, g.Tenant, g.Arr, g.OwnerID, g.DownloadID, reason)
	return err
}

func (r *Repo) PendingGrabSources(ctx context.Context, tenant string) ([]model.GrabSource, error) {
	rows, err := r.pool.Query(ctx, `SELECT tenant,arr,owner_id,download_id,infohash,magnet,torrent_url,source_title,source_kind FROM media_cache_grab_sources
 WHERE tenant=$1 AND NOT ready AND next_retry_at<=now() ORDER BY next_retry_at LIMIT 5`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.GrabSource
	for rows.Next() {
		var g model.GrabSource
		if err := rows.Scan(&g.Tenant, &g.Arr, &g.OwnerID, &g.DownloadID, &g.Torrent.InfoHash, &g.Torrent.Magnet, &g.Torrent.TorrentURL, &g.Torrent.SourceTitle, &g.Torrent.Kind); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (r *Repo) RecordGrabImport(ctx context.Context, g model.GrabSource, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO media_cache_grab_sources(tenant,arr,owner_id,download_id,imported_paths,imported_at)
 VALUES($1,$2,$3,lower($4),ARRAY[$5]::text[],now())
 ON CONFLICT(tenant,arr,owner_id,download_id) DO UPDATE SET
 imported_paths=ARRAY(SELECT DISTINCT unnest(media_cache_grab_sources.imported_paths||EXCLUDED.imported_paths)),imported_at=now()`, g.Tenant, g.Arr, g.OwnerID, g.DownloadID, path)
	return err
}

// Choose the most recent import for each exact path, including pending captures,
// so an older release cannot be attached while a newer source is still pending.
func (r *Repo) LinkGrabSources(ctx context.Context, tenant string) error {
	return r.linkGrabSources(ctx, tenant, "")
}
func (r *Repo) LinkGrabSourceForItem(ctx context.Context, tenant, id string) error {
	return r.linkGrabSources(ctx, tenant, id)
}
func (r *Repo) linkGrabSources(ctx context.Context, tenant, id string) error {
	_, err := r.pool.Exec(ctx, `WITH candidates AS (
 SELECT DISTINCT ON (m.id) m.id,g.* FROM media_cache_state m JOIN media_cache_grab_sources g
 ON g.tenant=m.tenant AND m.symlink_path=ANY(g.imported_paths)
 AND ((g.arr='radarr' AND m.media_type='movie' AND m.arr_id=g.owner_id)
 OR (g.arr='sonarr' AND m.media_type='series' AND m.series_id=g.owner_id))
 WHERE m.tenant=$1 AND ($2='' OR m.id=NULLIF($2,'')::uuid) ORDER BY m.id,g.imported_at DESC,g.created_at DESC
 ), applied AS (
 UPDATE media_cache_state m SET infohash=c.infohash,magnet=NULLIF(c.magnet,''),source_title=NULLIF(c.source_title,''),download_client='decypharr',source_download_id=c.download_id
 FROM candidates c WHERE m.id=c.id AND c.ready AND (m.source_download_id IS DISTINCT FROM c.download_id OR NOT EXISTS (
 SELECT 1 FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND s.source_kind=c.source_kind AND s.infohash=c.infohash))
 RETURNING m.id,c.infohash,c.filename,c.torrent_file,c.source_kind,c.magnet
 ) INSERT INTO media_cache_torrent_sources(media_id,infohash,filename,torrent_file,source_kind,magnet)
 SELECT id,infohash,filename,torrent_file,source_kind,magnet FROM applied
 ON CONFLICT(media_id) DO UPDATE SET infohash=EXCLUDED.infohash,filename=EXCLUDED.filename,torrent_file=EXCLUDED.torrent_file,source_kind=EXCLUDED.source_kind,magnet=EXCLUDED.magnet,updated_at=now()`, tenant, id)
	return err
}

// Do not restore/prune an older identity while the current import's capture is pending.
func (r *Repo) CurrentGrabSourcePending(ctx context.Context, id string) (bool, error) {
	var pending bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE((SELECT NOT g.ready OR m.source_download_id IS DISTINCT FROM g.download_id
 FROM media_cache_state m JOIN media_cache_grab_sources g ON g.tenant=m.tenant AND m.symlink_path=ANY(g.imported_paths)
 AND ((g.arr='radarr' AND m.media_type='movie' AND m.arr_id=g.owner_id) OR (g.arr='sonarr' AND m.media_type='series' AND m.series_id=g.owner_id))
 WHERE m.id=$1 ORDER BY g.imported_at DESC,g.created_at DESC LIMIT 1),false)`, id).Scan(&pending)
	return pending, err
}
