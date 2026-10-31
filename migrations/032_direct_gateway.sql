CREATE TABLE IF NOT EXISTS gateway_sources (
 id TEXT PRIMARY KEY DEFAULT ('source-'||gen_random_uuid()::text),
 tenant TEXT NOT NULL,
 protocol TEXT NOT NULL CHECK(protocol IN ('torrent','usenet')),
 kind TEXT NOT NULL CHECK(kind IN ('magnet','torrent_file','nzb_file')),
 fingerprint TEXT NOT NULL,
 filename TEXT NOT NULL DEFAULT '',
 payload BYTEA,
 magnet TEXT NOT NULL DEFAULT '',
 infohash TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant,protocol,fingerprint),
 CHECK((kind='magnet' AND protocol='torrent' AND magnet<>'' AND payload IS NULL)
 OR (kind='torrent_file' AND protocol='torrent' AND payload IS NOT NULL AND magnet='')
 OR (kind='nzb_file' AND protocol='usenet' AND payload IS NOT NULL AND magnet='' AND infohash=''))
);
CREATE TABLE IF NOT EXISTS gateway_jobs (
 LIKE media_cache_cloud_usenet INCLUDING ALL
);
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'usenet' CHECK(protocol IN ('torrent','usenet'));
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'nzb_file';
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS infohash TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS magnet TEXT NOT NULL DEFAULT '';
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS torrent_file BYTEA;
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS source_id TEXT REFERENCES gateway_sources(id);
CREATE UNIQUE INDEX IF NOT EXISTS gateway_torrent_identity ON gateway_jobs(tenant,infohash) WHERE protocol='torrent';
CREATE TABLE IF NOT EXISTS gateway_sessions (
 token_hash TEXT PRIMARY KEY,
 tenant TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
INSERT INTO gateway_sources(tenant,protocol,kind,fingerprint,filename,payload)
 SELECT DISTINCT ON (tenant,fingerprint) tenant,'usenet','nzb_file',fingerprint,filename,nzb
 FROM media_cache_cloud_usenet ORDER BY tenant,fingerprint,created_at
 ON CONFLICT(tenant,protocol,fingerprint) DO NOTHING;
INSERT INTO gateway_jobs(id,tenant,category,fingerprint,filename,nzb,name,provider_id,state,files,attempts,missing_checks,hidden,next_check_at,last_error,created_at,updated_at,source_id)
 SELECT j.id,j.tenant,j.category,j.fingerprint,j.filename,j.nzb,j.name,j.provider_id,j.state,j.files,j.attempts,j.missing_checks,j.hidden,j.next_check_at,j.last_error,j.created_at,j.updated_at,s.id
 FROM media_cache_cloud_usenet j JOIN gateway_sources s ON s.tenant=j.tenant AND s.protocol='usenet' AND s.fingerprint=j.fingerprint
 ON CONFLICT DO NOTHING;
