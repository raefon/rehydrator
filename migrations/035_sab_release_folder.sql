-- Persist the Arr-facing NZB release folder independently of provider/job naming.
ALTER TABLE gateway_jobs ADD COLUMN IF NOT EXISTS release_folder TEXT NOT NULL DEFAULT '';
-- Re-verify visible completed NZBs at their new staging path; never re-upload.
UPDATE gateway_jobs SET state='Mounting',catalog_revision=catalog_revision+1,verify_next_at=now()
WHERE protocol='usenet' AND state='Completed' AND NOT hidden AND release_folder='';
