-- NZB payloads, original Arr associations, and current opaque SAB job IDs.
ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS source_protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_state ADD COLUMN IF NOT EXISTS download_job_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS download_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS nzb_file BYTEA;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS nzb_filename TEXT NOT NULL DEFAULT '';
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'torrent';
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS nzb_file BYTEA;
ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS nzb_filename TEXT NOT NULL DEFAULT '';

ALTER TABLE media_cache_torrent_sources DROP CONSTRAINT IF EXISTS ck_saved_source_payload;
ALTER TABLE media_cache_torrent_sources ADD CONSTRAINT ck_saved_source_payload CHECK (
 (protocol='torrent' AND nzb_file IS NULL AND nzb_filename='' AND (
 (source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND filename<>'') OR
 (source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND filename='') OR
 (source_kind='recovery_required' AND torrent_file IS NULL AND magnet='' AND filename=''))) OR
 (protocol='usenet' AND source_kind='nzb_file' AND infohash='' AND torrent_file IS NULL AND filename='' AND magnet='' AND octet_length(nzb_file) BETWEEN 1 AND 52428800 AND nzb_file IS NOT NULL AND nzb_filename<>'' AND download_id<>''));
ALTER TABLE media_cache_grab_sources DROP CONSTRAINT IF EXISTS ck_grab_source_payload;
ALTER TABLE media_cache_grab_sources ADD CONSTRAINT ck_grab_source_payload CHECK (
 (protocol='torrent' AND nzb_file IS NULL AND nzb_filename='' AND (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','torrent_file','magnet')) OR
 (ready AND source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND torrent_url='' AND filename<>'') OR
 (ready AND source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND torrent_url='' AND filename=''))) OR
 (protocol='usenet' AND infohash='' AND torrent_file IS NULL AND filename='' AND magnet='' AND (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','nzb_file')) OR
 (ready AND source_kind='nzb_file' AND torrent_url='' AND octet_length(nzb_file) BETWEEN 1 AND 52428800 AND nzb_file IS NOT NULL AND nzb_filename<>''))));

-- One current runtime job per original source, shared by season-pack episodes.
CREATE TABLE IF NOT EXISTS media_cache_usenet_jobs (
 tenant TEXT NOT NULL, arr TEXT NOT NULL CHECK(arr IN ('radarr','sonarr')),
 owner_id INTEGER NOT NULL CHECK(owner_id>0), original_download_id TEXT NOT NULL,
 job_id TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant,arr,owner_id,original_download_id)
);
