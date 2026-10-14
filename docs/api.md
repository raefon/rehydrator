# API

Most `/api/*` endpoints require:

```text
Authorization: Bearer <REHYDRATOR_API_TOKEN>
```

Some webhook endpoints also accept `?token=<REHYDRATOR_API_TOKEN>` for Plex/Radarr-style webhook compatibility.

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
POST /api/playback/plex
POST /api/playback/event
```

Generic playback JSON:

```json
{
  "source": "manual-test",
  "event": "media.play",
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

## Plex hygiene

```text
GET  /api/plex/refreshes/recent?limit=20
POST /api/plex/refresh/movie/{radarr_id}
POST /api/plex/refresh/movies
```

Use these to clear stale Plex unavailable indicators after Rehydrator restores files. `GET /api/plex/refreshes/recent` shows the latest refresh audit rows, including dedupe skips.

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

Checks configured runtime dependencies such as Postgres, Radarr, Decypharr, Seerr, and Plex. It returns HTTP 200 when all checked dependencies pass and HTTP 503 when one or more fail.

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

## Plex refresh behavior

`POST /api/plex/refresh/movie/{radarr_id}` now refreshes the movie folder path only, not the whole Plex library section. Whole-section refresh remains available through `POST /api/plex/refresh/movies`, but it should be used sparingly.

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

## Plex refresh dedupe

`plex.refresh_dedupe_minutes` suppresses duplicate automated refreshes for the same media item and folder path. This prevents `rehydration_complete` and `self_heal_available` from firing two Plex scans for the same movie folder in the same minute. Manual movie and whole-section refresh endpoints remain available for repair work.

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
  http://localhost:8080/api/plex/refresh/series/<EPISODE_FILE_ID>

curl -i -X POST -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/plex/refresh/tv

curl -s -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/admin/sonarr/status | jq

curl -s -H "Authorization: Bearer $API_TOKEN" \
  "http://localhost:8080/api/admin/sonarr/episode-files?limit=100" | jq
```


## Webhook diagnostics (v0.4.7)

The Radarr and Sonarr webhook endpoints emit structured application logs for authenticated event receipt and processing outcomes. Look for messages such as `sonarr webhook received`, `sonarr webhook processed`, `sonarr webhook refresh completed`, `sonarr webhook refresh skipped by debounce`, and `sonarr deletion webhook processed`. Rejected method, authentication, and JSON requests are logged at warning level. `Test` events return HTTP 202 with `action: test_acknowledged` and do not trigger refreshes. Tokens, query strings, and raw webhook payloads are not written to these logs.
