# API

Most `/api/*` endpoints require:

```text
Authorization: Bearer <REHYDRATOR_API_TOKEN>
```

Some webhook endpoints also accept `?token=<REHYDRATOR_API_TOKEN>` for Jellyfin/Radarr-style webhook compatibility.

## Health and metrics

```text
GET /healthz
GET /readyz
GET /metrics
```

## State

```text
GET /api/state
GET /api/state/movie/{radarr_id}
```

## Re-arm and prune

```text
POST /api/rearm/movie/{radarr_id}
POST /api/rearm/movie/tmdb/{tmdb_id}
POST /api/prune/movie/{radarr_id}
POST /api/prune/movie/{radarr_id}?dry_run=true
```

## Playback

```text
POST /api/playback/jellyfin
POST /api/playback/event
```

Generic playback JSON:

```json
{
  "source": "manual-test",
  "event": "PlaybackStart",
  "media_type": "movie",
  "tmdb_id": 1218925,
  "title": "Chainsaw Man - The Movie: Reze Arc",
  "user": "nofear101"
}
```

## Webhooks and refresh

```text
POST /api/radarr/webhook
POST /api/sonarr/webhook
POST /api/seerr/webhook
POST /api/refresh/radarr
POST /api/refresh/seerr
```


## Arr deletion reconciliation (v0.4.6)

Radarr `MovieDelete` / `MovieDeleted` and Sonarr `SeriesDelete` / `SeriesDeleted` webhooks immediately remove the corresponding active Rehydrator rows. A tombstone is retained in `media_cache_tombstones` with the Arr app, identifier, title/external ID when known, reason, payload metadata, and timestamp. This is an audit trail only; it does not delete files or provider-side torrents.

The periodic full inventory sync is the safety net if a webhook is missed:

- Radarr removes tracked movie rows whose Radarr movie ID is absent from a successfully fetched full movie inventory.
- Sonarr removes tracked episode-file rows if the series or episode-file ID is absent from a complete inventory. If any episode or episode-file inventory call fails, Sonarr skips pruning for that sync.
- API errors do not count as an empty inventory. Requested placeholders are retained.
- When an item is added to Arr again (including via Seerr/watchlist integrations), the normal sync recreates its active tracking rows. Re-arming still uses the normal playback/Seerr workflow; deletion never triggers a download by itself.

Configure a Webhook connection in Radarr at **Settings → Connect → Webhook**, select **On Movie Delete**, and point it at `http://<rehydrator-host>:8080/api/radarr/webhook`. Configure Sonarr similarly with **On Series Delete** and `http://<rehydrator-host>:8080/api/sonarr/webhook`. If API-token authentication is enabled, configure the webhook's supported authentication to send `Authorization: Bearer <token>` or use the supported `?token=<token>` compatibility option. Restrict the endpoint to your trusted network.

## Jellyfin hygiene

```text
GET  /api/jellyfin/refreshes/recent?limit=20
POST /api/jellyfin/refresh/movie/{radarr_id}
POST /api/jellyfin/refresh/library
```

Use these to clear stale Jellyfin unavailable indicators after Rehydrator restores files. `GET /api/jellyfin/refreshes/recent` shows the latest refresh audit rows, including dedupe skips.

## Operational / admin endpoints

```text
GET  /api/state/summary
GET  /api/health/dependencies
GET  /api/admin/cooldowns
POST /api/admin/retry-failed?limit=10
POST /api/admin/rearm-all
```

### GET /api/state/summary

Returns counts by lifecycle state for the current tenant. This is the fastest way to see whether items are piling up in `WAITING_FOR_VISIBILITY`, `FAILED`, or `ARCHIVED`.

### GET /api/health/dependencies

Checks configured runtime dependencies such as Postgres, Radarr, Decypharr, Seerr, and Jellyfin. It returns HTTP 200 when all checked dependencies pass and HTTP 503 when one or more fail.

### GET /api/admin/cooldowns

Shows active provider cooldowns. This is useful after TorBox/Decypharr rate limits or 429 responses.

### POST /api/admin/rearm-all

Queues a full re-arm for valid tracked Radarr movie and Sonarr episode-file rows. It uses the normal bounded re-arm worker and existing CSI visibility checks. A dry run is the default and makes no changes:

```bash
curl -sS -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/admin/rearm-all | jq
```

To queue the eligible rows, explicitly confirm the operation:

```bash
curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  "http://localhost:8080/api/admin/rearm-all?confirm=true"
```

This resets retry delays for eligible rows and queues them through the regular Arr/Decypharr path. It excludes `REQUESTED` placeholders, in-flight `REARMING`/`PRUNING` rows, and rows without a valid Arr ID or symlink path. It does **not** delete TorBox torrents or prune records. A visible CSI path is handled by the existing visibility short-circuit when enabled.

### POST /api/admin/retry-failed?limit=10

Clears retry counters and retry delay for `BROKEN`, `FAILED`, and `WAITING_FOR_VISIBILITY` rows. For failed/broken rows it also requests re-arm again. Use this after fixing provider credentials, rate limits, or CSI/rclone visibility issues.

## Jellyfin refresh behavior

`POST /api/jellyfin/refresh/movie/{radarr_id}` notifies Jellyfin of the tracked movie file path. A scan of all libraries remains available through `POST /api/jellyfin/refresh/library`, but it should be used sparingly.

## Metadata guardrail admin endpoints

### GET `/api/admin/invalid-rows`

Returns rows that look unsafe for prune/re-arm work, such as placeholder IDs, rearmable states with no `symlink_path`, or available/archive rows with missing `infohash`.

```bash
curl -s -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/admin/invalid-rows?limit=100 | jq
```

### POST `/api/admin/self-heal/run`

Runs one self-heal pass immediately instead of waiting for the scheduled interval.

```bash
curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/admin/self-heal/run
```

## Jellyfin refresh dedupe

`jellyfin.refresh_dedupe_minutes` suppresses duplicate automated refreshes for the same media item and file path. This prevents `rehydration_complete` and `self_heal_available` from firing two Jellyfin scans for the same movie file in the same minute. Manual movie, episode, and whole-library refresh endpoints remain available for repair work.

## v0.4.1 Sonarr / series endpoints

```bash
curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/refresh/sonarr

curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/rearm/series/<EPISODE_FILE_ID>

curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/prune/series/<EPISODE_FILE_ID>

curl -s -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/state/series/<EPISODE_FILE_ID> | jq

curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/jellyfin/refresh/series/<EPISODE_FILE_ID>

curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/jellyfin/refresh/library

curl -s -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/admin/sonarr/status | jq

curl -s -H "Authorization: Bearer $API_TOKEN" \
  "http://localhost:8080/api/admin/sonarr/episode-files?limit=100" | jq
```


## Webhook diagnostics (v0.4.7)

The Radarr and Sonarr webhook endpoints emit structured application logs for authenticated event receipt and processing outcomes. Look for messages such as `sonarr webhook received`, `sonarr webhook processed`, `sonarr webhook refresh completed`, `sonarr webhook refresh skipped by debounce`, and `sonarr deletion webhook processed`. Rejected method, authentication, and JSON requests are logged at warning level. `Test` events return HTTP 202 with `action: test_acknowledged` and do not trigger refreshes. Tokens, query strings, and raw webhook payloads are not written to these logs.

## Grab source capture and import linking (v0.5.6)

Enable **On Grab** and **On Import** in the existing Radarr/Sonarr Webhook
connections targeting `/api/radarr/webhook` and `/api/sonarr/webhook`. Upgrades
must also send their import notifications. Keep existing API authentication.

`Grab` events persist a capture job keyed by tenant, Arr, movie/series owner ID,
and `downloadId`. Rehydrator attempts source capture immediately; delayed Arr
history or an unavailable indexer leaves durable work for the reconciliation
worker to retry. HTTP 202 with `action: grab_source_queued` means the job was
persisted, not necessarily that the file has already been captured. Duplicate
completed grabs do not fetch the indexer again.

`Download` (import) events associate `downloadId` and the imported file path with
that capture. Association survives an import arriving before sync creates the
media row, or before capture finishes. One season-pack source can attach to
multiple imported episode files. Owners and exact paths must match; titles are
not used to select a captured release. Apply migrations 020–023 before enabling this.


Saved submission sources are typed as `torrent_file` or `magnet`, with mutually
exclusive payloads. A hash-only record is not submitted as a generated magnet.
If its original source cannot be recovered, the rearm request is cleared and
its row reports `FAILED` with `last_error` explaining that source recovery is
required. Capture the original source, then explicitly request rearm again.

The global scan endpoint is `POST /api/jellyfin/refresh/library`; it scans all libraries, rather than a movie/TV section. Targeted endpoints notify the tracked file path.

## Usenet lifecycle (v0.6.0)

Existing Arr Grab and Download/import webhook routes also accept explicit Usenet
protocol/client metadata. No new public submission endpoint is introduced.
The state inventory includes `SourceProtocol`, `SourceDownloadID`, and
`DownloadJobID`; NZB sources do not have torrent infohashes. Existing rearm routes
use saved NZB bytes and Decypharr's SAB-compatible job status automatically.
See [Usenet setup](usenet-setup.md) for configuration and recovery limits.
