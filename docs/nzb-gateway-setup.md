# Hosted NZBs through the shared gateway

Use the existing Rehydrator gateway/controller and CSI mount. This adds a
SAB-compatible client endpoint to Arr; it does not require a separate SABnzbd
application, Decypharr NNTP server or another WebDAV mount.

TorBox Pro provides the cloud Usenet downloader. Rehydrator uploads the original
NZB, requests normal scheduling and post-processing `-1` (TorBox's recommended
default for repair/extraction and removal of source archives). See the
[official TorBox Usenet documentation](https://github.com/TorBox-App/torbox-sdk-go/blob/main/documentation/services/usenet_service.md).

## Gateway credentials

Keep the direct gateway configuration:

```yaml
role: gateway
gateway:
  enabled: true
  api_key: ""     # Existing TorBox Pro API key, supplied through your Secret
  sab_api_key: "" # Separate client key for Sonarr/Radarr, not your TorBox key
```

This is a fragment; retain the existing database, tenant, qBittorrent and DAV
settings. `GATEWAY_SAB_API_KEY` overrides `gateway.sab_api_key`. Restart the gateway
after updating its configuration Secret.
The controller needs no additional credentials for hosted NZBs.

For the tightened SAB client responses and acceptance logs, deploy v0.7.5 or
later. Apply the current schema through migration 035.

## Sonarr and Radarr

Add a download client of type **SABnzbd** alongside the working qBittorrent client:

| Setting | Sonarr | Radarr |
|---|---|---|
| Name | Rehydrator Usenet | Rehydrator Usenet |
| Host | `tenet-rehydrator-gateway` | `tenet-rehydrator-gateway` |
| Port | `8080` | `8080` |
| SSL | Off for the internal HTTP Service | Off for the internal HTTP Service |
| URL Base | `/sabnzbd` | `/sabnzbd` |
| API Key | Gateway SAB client key | Gateway SAB client key |
| Category | `sonarr` | `radarr` |

Username/password can remain blank. If category names were customized, use the
configured names instead. Use **Test**, then save the client. Keep completed
download handling enabled and retain the existing Arr grab/import webhooks to
the tenant controller. The SAB add request uses the original multipart NZB, and
completion history reports a separate per-job storage path as expected by the
[Arr SAB client](https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/Download/Clients/Sabnzbd/Sabnzbd.cs).

Ensure Prowlarr has a working NZB indexer synced to the relevant Arr application.
For this test, manually select one NZB release in Arr's interactive search so
release preference settings do not choose a torrent instead. Changing download
client priority alone does not select a protocol for the release search.

## First live check

1. Submit one small NZB release through Sonarr or Radarr.
2. Gateway logs should show `SAB NZB accepted`, followed by
   `TorBox gateway submission result` with `protocol=usenet`.
3. Confirm the existing cloud download in the TorBox dashboard. Allow time for
   downloading, verification and extraction; acceptance is not completion.
4. The catalog becomes visible at
   `/storage/torbox/usenet/cloud-nzb-UUID/` when the provider is ready.
5. The controller stages links below
   `/storage/media/downloads/rehydrator/sonarr/cloud-nzb-UUID/` (or `radarr`) and
   checks a mounted payload read. Look for `TorBox gateway ready for Arr import`.
6. Arr imports the item; then confirm Jellyfin indexing, playback and seeking.

Both the controller and Arr must see the same writable staging directory and
readable `/storage/torbox` mount. Sonarr/Radarr should use the same import/symlink
behavior already proven for torrents. Do not move remote media into local
storage unintentionally.

Check the tenant database without exposing source payloads:

```sql
SELECT id, category, state, provider_id, attempts, next_check_at, last_error
FROM gateway_jobs
WHERE tenant = 'tenet-nofear101' AND protocol = 'usenet'
ORDER BY created_at DESC
LIMIT 10;
```

- `Queued`: waiting for the shared submission budget/worker.
- `Downloading`: TorBox accepted an active Usenet ID; waiting for provider readiness.
- `Mounting`: provider ready, waiting for controller/CSI verification.
- `Completed`: SAB history exposes the staged import directory to Arr.
- `Failed`: inspect the provider's error and acquire a replacement in Arr as needed.
- `SubmissionUnknown`: inspect the existing TorBox item; do not reset/re-add blindly.

Torrent and NZB submissions share the account-owner submission gap. Queue-only
provider responses retain their queue ID in the error message and are not treated
as active download IDs. Automatic exact-hash reconciliation applies to torrents;
NZBs have no torrent hash, so unknown NZB outcomes still require inspection.

History deletion hides the client job while preserving its original NZB and DAV
catalog. Regular provider checks can recover confirmed missing placements using
the retained source within the configured recovery budget. A failed article
retrieval, expired/removed article or provider-side unpack error may require a
replacement NZB rather than another attempt of the same source.

## Acceptance status

Automated tests cover Sonarr/Radarr request shapes, category separation, original
NZB retention, split-role readiness, staged import paths and retained catalog after
history cleanup. They use mocked TorBox responses and simulated mounted files.
Live TorBox Usenet completion, actual Arr import and Jellyfin playback must be
confirmed with the first real NZB; torrent success alone does not verify them.

## Release-named import folders (v0.8.1)

SAB history reports `storage` as
`<download_path>/<category>/<job-id>/<release-folder>/`. The job ID remains the
stable download identity. Release names are sanitized and persisted when the
controller first stages a provider-ready NZB, then remain fixed across restarts,
provider name changes and recovery. Provider-relative filenames are preserved.

Apply migration 035 before rolling out both roles. Existing visible completed
NZBs return to Mounting until the controller creates and verifies the new staging
path. No provider re-download is requested, original WebDAV paths remain valid,
and old staging links are left untouched. Sonarr should retry its queue after
completion is reported again. Hidden jobs remain retained without restaging unless
made visible again. Existing staging conflicts block completion rather than
replacing files or following unsafe parent symlinks.
