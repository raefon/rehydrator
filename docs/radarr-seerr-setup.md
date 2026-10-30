# Radarr and Seerr Setup

## Radarr

Radarr provides imported movie paths, TMDb IDs, and download history.

Recommended Radarr Connect webhook URL:

```text
http://tenet-rehydrator:8080/api/radarr/webhook?token=<REHYDRATOR_API_TOKEN>
```

Enable these Radarr events:

```text
On Movie Added
On Download/Import
On Upgrade
```

The webhook lets Rehydrator seed or update rows immediately instead of waiting for polling.

## Radarr naming

For new imports, use stable filenames with TMDb IDs. Example folder format:

```text
{Movie CleanTitle} ({Release Year}) [tmdb-{TmdbId}]
```

Example file format:

```text
{Movie CleanTitle} ({Release Year}) [tmdb-{TmdbId}] - {Quality Full}-{Release Group}
```

Avoid bulk-renaming a large existing library until the system is stable, because path churn can trigger Jellyfin and CSI-rclone noise.

## Seerr

Seerr request sync/webhooks can create placeholder rows before Radarr import completes.

Recommended webhook URL:

```text
http://tenet-rehydrator:8080/api/seerr/webhook?token=<REHYDRATOR_API_TOKEN>
```

Seerr test notifications return HTTP 200 after authentication without creating tracking rows or triggering rearming, so the webhook can be tested and saved. Include `"notification_type": "{{notification_type}}"` in the JSON payload. Real movie notifications still require a TMDB or Arr ID.

Seerr sync config:

```yaml
seerr:
  sync:
    enabled: true
    interval_seconds: 300
    limit: 100
```

## Placeholder promotion

When Seerr sees a new request, Rehydrator can create a temporary row. When Radarr imports the movie, Rehydrator promotes that row to the real Radarr movie ID.

## TV requests and Moonfin (v0.6.11)

Moonfin requests go to Seerr, then Seerr sends approved acquisitions to Sonarr or
Radarr. Rehydrator does not intercept Moonfin's request response or change its
permissions/approval behavior.

Keep Sonarr and Radarr configured as Seerr services with automatic search on
approval as desired, and keep Rehydrator's Grab and Download/import webhooks
enabled in each Arr. For Rehydrator's Seerr notification agent, enable approved
and auto-approved request events. Use the existing authenticated webhook URL
with a payload including the following fields:

```json
{
  "notification_type": "{{notification_type}}",
  "requestId": "{{request_id}}",
  "media": {
    "mediaType": "{{media_type}}",
    "tmdbId": "{{media_tmdbid}}",
    "tvdbId": "{{media_tvdbid}}"
  }
}
```

This minimal webhook records TV requests. Keep Seerr polling enabled: the
`/api/v1/request` response supplies requested seasons and numeric request status,
so polling can rearm only approved requests in that exact scope. An explicit
`seasons: [1, 3]` webhook field is also supported. Missing scope never means the
entire show. TVDB show IDs, Sonarr series IDs, and episode-file IDs are distinct.
Seerr TV requests do not create episode-file placeholders.

Only archived/broken/failed imports with matching saved sources rearm. Multiple
episodes from a pack share one primary rearm and visibility waiting. A request
is consumed once, even with no matching source; new/source-less acquisitions
remain Seerr/Sonarr's responsibility. Replaying the same request ID does not
retry it. Pending/declined/completed requests do not trigger new TV recovery.

### Make source-less missing files requestable again

By default, source-less imports remain FAILED in Rehydrator without Arr changes.
To opt into individual Arr file removal:

```yaml
jellyfin:
  inventory_sync:
    mark_missing_in_arr: true
file_health:
  canary_path: "/storage/media/path/to/a/known-readable-torrent-backed-file.mkv"
```

The canary must use the same remote mount and must not be an NNTP-backed NZB.
This intentionally reads the canary byte; it does not probe Usenet imports.
On each inventory pass, previously source-recovery-required torrent rows are
rechecked. Only dangling symlinks qualify. The current Arr file identity and
path must match, and absence is checked again immediately before invoking
`DELETE /episodefile/{episodeFileId}` or `DELETE /moviefile/{movieFileId}`.
These are Arr file-deletion APIs, not database-only flags. Restored/replaced
files are skipped; HTTP errors leave the job for investigation/retry.
Arr then treats the import as absent while retaining the show/movie. Monitoring
is not changed; review Arr's own handling of manually removed media.
Existing Arr deletion webhooks/inventory cleanup may remove Rehydrator's old
tracking row; replacement Grab/Import events create the new tracking/source.

After that, run Seerr's applicable Sonarr/Radarr/Jellyfin scan jobs and verify the
season/movie is requestable. Existing completed requests or stale Seerr media
availability can still block a second request: review that item's request and
availability in Seerr, using its admin controls if necessary. Rehydrator does
not delete Seerr requests or force its availability flags. Reload Moonfin's
details page after Seerr updates.

### Verification

1. Use one missing episode/season as a canary; confirm the mount is healthy.
2. Enable the opt-in Arr update and watch for
   `Arr imported file removed after confirmed missing target`.
3. Verify Sonarr/Radarr has no imported-file record for it, with monitoring as
   intended. Verify Seerr shows the season/movie as requestable.
4. Request it from Moonfin. Confirm the new request appears in Seerr and is
   approved; look for `Seerr TV request reconciled` or the TV webhook response.
5. Confirm the requested scope is downloaded/rearmed, the new source is saved,
   Arr imports it, and Jellyfin accepts the refresh.
6. Requests without saved sources should be acquired by Sonarr, not repeatedly
   sent to Decypharr by Rehydrator.

Local integration tests cover Seerr API-shaped requests, approval gating,
season-scoped source validation, webhook/poll replay, and Arr file ID/path
checks. Live Moonfin/Seerr permissions and availability must be checked in the
deployment; no live requests were submitted by this change.

Sources:
- https://github.com/seerr-team/seerr/blob/develop/server/lib/notifications/agents/webhook.ts
- https://github.com/seerr-team/seerr/blob/develop/server/constants/media.ts
- https://github.com/Moonfin-Client/Moonfin-Core/wiki/User-Guide
- https://github.com/Sonarr/Sonarr/blob/develop/src/NzbDrone.Core/MediaFiles/MediaFileDeletionService.cs
- https://github.com/Radarr/Radarr/blob/develop/src/NzbDrone.Core/MediaFiles/MediaFileDeletionService.cs
