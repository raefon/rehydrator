# TorBox Pro hosted NZBs (v0.7.0)

This backend uploads original NZBs directly to TorBox. TorBox downloads, repairs
and unpacks them server-side. Rehydrator exposes completed files through a
read-only WebDAV endpoint for your rclone/CSI mount, stages download symlinks,
and reports completion to Sonarr/Radarr through a separate SAB-compatible API.
No NNTP article streaming is used for new cloud jobs.

The implementation is local and tested with a mocked TorBox API and simulated
mount. A live TorBox/CSI/Arr canary is required before bulk cutover.

## 1. Deploy and configure

Back up PostgreSQL. Deploy v0.7.0 with migration 031_cloud_usenet.sql after
migrations 001–030. Automatic schema initialization includes it when
db_auto_migrate is enabled.

Merge this into the existing configuration:

~~~yaml
rearm_enabled: true
cloud_usenet:
  enabled: true
  api_key: ""       # Set CLOUD_USENET_API_KEY from a Secret.
  sab_api_key: ""   # Set CLOUD_USENET_SAB_API_KEY from a Secret.
  dav_username: ""  # Set CLOUD_USENET_DAV_USERNAME from a Secret.
  dav_password: ""  # Set CLOUD_USENET_DAV_PASSWORD from a Secret.
  mount_path: /storage/torbox-cloud
  download_path: /storage/media/downloads/torbox-usenet
  submit_interval_seconds: 120
  recover_missing: true
~~~

Use the TorBox Pro REST API key, not news-server credentials. Choose separate
credentials for SAB and WebDAV. Existing torrent-health credentials need not
change. API serving must remain enabled.

All fields have CLOUD_USENET_ environment overrides, shown in .env.example.
The download path must be shared writable storage visible at the same path to
Rehydrator and Arr. The cloud mount must be visible to Rehydrator, Arr and
Jellyfin. Keep staging outside the read-only cloud mount.

## 2. Add a separate rclone/CSI mount

Use config/torbox-cloud-rclone.example.conf as a new remote. Replace its
credentials with the configured WebDAV user/password; rclone's pass field takes
an obscured password (rclone config can generate this). Do not put the TorBox API
key in rclone configuration.

Endpoint:

~~~text
http://tenet-rehydrator.tenet-nofear101.svc.cluster.local:8080/cloud-usenet/dav/
~~~

Mount at /storage/torbox-cloud. Leave the Decypharr mount and NNTP files untouched.
For a standalone test, equivalent rclone options are:

~~~bash
rclone mount torbox-cloud: /storage/torbox-cloud \
  --allow-other \
  --dir-cache-time 30s \
  --poll-interval 0 \
  --attr-timeout 1s \
  --vfs-cache-mode full \
  --vfs-cache-max-size 20G \
  --vfs-read-chunk-size 8M \
  --vfs-read-chunk-size-limit 64M \
  --buffer-size 8M
~~~

Adapt cache size/ownership to your node and apply equivalent options to the CSI
volume if using Kubernetes. Share the mount into all media containers. This
repository does not contain your cluster chart; deploying the new PV/PVC and
application mounts is a separate cluster change.

The DAV root exposes jobs/cloud-nzb-UUID/... paths, stable across provider
re-adds. HEAD and listings use stored metadata. GET proxies HTTP ranges and caches
delivery URLs for 20 minutes, refreshing once after a 401/403. Credentials and
expiring provider URLs remain server-side.

## 3. Add an Arr download client

In Sonarr and Radarr, add a new SABnzbd client:

| Setting | Value |
|---|---|
| Name | TorBox Cloud NZB |
| Host | tenet-rehydrator.tenet-nofear101.svc.cluster.local |
| Port | 8080 |
| URL Base | /sabnzbd-cloud |
| API Key | CLOUD_USENET_SAB_API_KEY |
| Category | sonarr / radarr, matching existing Rehydrator categories |

Use a canary tag/client routing rule initially. Test the connection. Keep
Completed Download Handling enabled and preserve Rehydrator's Grab and
Download/import webhooks. Prowlarr/Arr still select releases; this backend
does not provide an indexer.

The original upload is stored before its SAB job ID is returned. Namespaced
cloud-nzb-UUID identifiers select this backend; TorBox's numeric Usenet ID is
separate. Grab capture retrieves retained bytes directly, so Prowlarr does not
need to keep the original download URL alive.

## 4. Verify one acquisition end to end

1. Grab one small authorized NZB through Arr using the new client.
2. Look for "TorBox cloud NZB submission result" and verify its provider ID appears
   in TorBox's cloud dashboard.
3. Wait for completion/repair. Rehydrator publishes virtual files and stages links
   under /storage/media/downloads/torbox-usenet/CATEGORY/JOB-ID/.
4. Confirm links target /storage/torbox-cloud/jobs/... and are readable inside
   Arr and Jellyfin. Completion requires provider completion, a valid catalog,
   successful staging and a bounded mounted-file read.
5. Look for "TorBox cloud NZB ready for Arr import", then Arr import and
   Rehydrator source-capture/Jellyfin refresh logs.
6. Verify the imported source is usenet / nzb_file and its original download ID
   starts with cloud-nzb-.

Database status:

~~~sql
SELECT id, category, name, provider_id, state, attempts,
       missing_checks, next_check_at, last_error
FROM media_cache_cloud_usenet
WHERE tenant = 'tenet-nofear101'
ORDER BY created_at DESC;
~~~

Imported jobs:

~~~sql
SELECT m.media_type, m.arr_id, m.source_protocol,
       m.source_download_id, m.download_job_id, s.source_kind
FROM media_cache_state m
LEFT JOIN media_cache_torrent_sources s ON s.media_id = m.id
WHERE m.tenant = 'tenet-nofear101'
  AND m.source_download_id LIKE 'cloud-nzb-%';
~~~

## Recovery and limits

- The worker claims at most five jobs per 15-second tick. PostgreSQL coordinates
  submissions across replicas, with a default two-minute gap per tenant
  (30 uploads/hour). Other apps share the TorBox account's limits.
- Completed jobs receive metadata-only checks, not repeated file-byte probes.
  Three fresh missing observations confirm loss, spaced five minutes apart.
  Initial status checks run about once per minute when worker capacity permits.
- With recover_missing and rearm_enabled enabled, retained NZBs restore missing
  placements using the stable job path. Related imports enter visibility waiting,
  then normal readability/Jellyfin refresh handling.
- Each job permits three total upload reservations, including its initial upload.
  API errors are not evidence of eviction. A rejected/indeterminate upload becomes
  SubmissionUnknown for inspection (reported as a paused queue item, not a failed release). Interrupted Submitting leases are also flagged
  unknown after five minutes; no blind resubmission occurs. Check TorBox before
  manually changing any reservation.
- Provider-reported failed NZBs become Failed. Acquire a replacement through Arr.
- Queue/history removal hides the entry but preserves NZB bytes, virtual files,
  staging files and TorBox placement. Cancel/delete cloud downloads through TorBox
  itself when necessary.
- Duplicate NZB bytes within a category reuse the stable job and do not bypass
  attempt limits.
- No hosted-Usenet pruning is enabled. Torrent-only file-health sampling and old
  NNTP behavior remain unchanged.

Mount cache refreshes and provider operations are asynchronous; completion is not
instantaneous. This does not guarantee permanent provider retention.

## Migrating from NNTP

After the canary passes, route new NZB acquisitions to the cloud client. Drain the
old Decypharr NZB queue before disabling its download-client entry for new adds.
Keep Decypharr's NNTP configuration and mount for existing NNTP imports. Keep
torrents on the existing qBittorrent client.

Do not relabel old jobs or rewrite symlinks globally. Existing NNTP identifiers
continue through their old status/rearm path. Migrating an old import requires
deliberate reacquisition or a separate source/path migration.

## References

- [TorBox API](https://api.torbox.app/docs): Usenet create, mylist and requestdl.
- [TorBox SDK Usenet documentation](https://github.com/TorBox-App/torbox-sdk-go/blob/main/documentation/services/usenet_service.md).
- [TorBox API rate limits](https://support.torbox.app/en/articles/13726368-api-rate-limits).
- [Sonarr's SAB API contract](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/Clients/Sabnzbd/SabnzbdProxy.cs).
