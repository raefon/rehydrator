-- Submission source and release identity are independent. Never synthesize magnets.
ALTER TABLE media_cache_torrent_sources ALTER COLUMN torrent_file DROP NOT NULL;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
ALTER TABLE media_cache_torrent_sources ADD COLUMN IF NOT EXISTS magnet TEXT NOT NULL DEFAULT '';
UPDATE media_cache_torrent_sources SET source_kind='torrent_file' WHERE source_kind IS NULL;
UPDATE media_cache_torrent_sources SET filename='download.torrent' WHERE source_kind='torrent_file' AND filename='';
ALTER TABLE media_cache_torrent_sources ALTER COLUMN source_kind SET DEFAULT 'recovery_required';
ALTER TABLE media_cache_torrent_sources ALTER COLUMN source_kind SET NOT NULL;

-- The old code generated bare xt-only magnets. Their original provenance cannot
-- be established. Preserve the legacy value on media_cache_state for audit, but
-- require source recovery instead of treating it as a proven magnet source.
INSERT INTO media_cache_torrent_sources(media_id,infohash,filename,torrent_file,source_kind,magnet)
SELECT id,infohash,'',NULL,
 CASE WHEN magnet ~* '^magnet:.*&' THEN 'magnet' ELSE 'recovery_required' END,
 CASE WHEN magnet ~* '^magnet:.*&' THEN magnet ELSE '' END
FROM media_cache_state WHERE infohash IS NOT NULL AND infohash<>''
ON CONFLICT(media_id) DO NOTHING;

ALTER TABLE media_cache_grab_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
UPDATE media_cache_grab_sources SET source_kind=CASE
 WHEN NOT ready THEN 'unresolved'
 WHEN torrent_file IS NOT NULL THEN 'torrent_file'
 WHEN magnet ~* '^magnet:.*&' THEN 'magnet'
 ELSE 'recovery_required' END WHERE source_kind IS NULL;
UPDATE media_cache_grab_sources SET ready=false,last_error='source recovery required: legacy source provenance unknown'
 WHERE source_kind='recovery_required' AND ready;
UPDATE media_cache_grab_sources SET magnet='',torrent_url='' WHERE source_kind='torrent_file' AND ready;
UPDATE media_cache_grab_sources SET filename='download.torrent' WHERE source_kind='torrent_file' AND ready AND filename='';
UPDATE media_cache_grab_sources SET magnet='' WHERE source_kind='unresolved' AND torrent_url<>'';
UPDATE media_cache_state m SET magnet=NULL FROM media_cache_torrent_sources s WHERE s.media_id=m.id AND s.source_kind='torrent_file' AND s.infohash=m.infohash;
ALTER TABLE media_cache_grab_sources ALTER COLUMN source_kind SET DEFAULT 'unresolved';
ALTER TABLE media_cache_grab_sources ALTER COLUMN source_kind SET NOT NULL;

DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_saved_source_payload' AND conrelid='media_cache_torrent_sources'::regclass) THEN
 ALTER TABLE media_cache_torrent_sources ADD CONSTRAINT ck_saved_source_payload CHECK (
 (source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND filename<>'') OR
 (source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND filename='') OR
 (source_kind='recovery_required' AND torrent_file IS NULL AND magnet='' AND filename=''));
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='ck_grab_source_payload' AND conrelid='media_cache_grab_sources'::regclass) THEN
 ALTER TABLE media_cache_grab_sources ADD CONSTRAINT ck_grab_source_payload CHECK (
 (NOT ready AND source_kind IN ('unresolved','recovery_required','torrent_file','magnet')) OR
 (ready AND source_kind='torrent_file' AND torrent_file IS NOT NULL AND magnet='' AND torrent_url='' AND filename<>'') OR
 (ready AND source_kind='magnet' AND torrent_file IS NULL AND magnet<>'' AND torrent_url='' AND filename=''));
 END IF;
END $$;
