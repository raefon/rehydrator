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
POST /api/seerr/webhook
POST /api/refresh/radarr
POST /api/refresh/seerr
```

## Plex hygiene

```text
POST /api/plex/refresh/movie/{radarr_id}
POST /api/plex/refresh/movies
```

Use these to clear stale Plex unavailable indicators after Rehydrator restores files.

## Operational / admin endpoints

```text
GET  /api/state/summary
GET  /api/health/dependencies
GET  /api/admin/cooldowns
POST /api/admin/retry-failed?limit=10
```

### GET /api/state/summary

Returns counts by lifecycle state for the current tenant. This is the fastest way to see whether items are piling up in `WAITING_FOR_VISIBILITY`, `FAILED`, or `ARCHIVED`.

### GET /api/health/dependencies

Checks configured runtime dependencies such as Postgres, Radarr, Decypharr, Seerr, and Plex. It returns HTTP 200 when all checked dependencies pass and HTTP 503 when one or more fail.

### GET /api/admin/cooldowns

Shows active provider cooldowns. This is useful after TorBox/Decypharr rate limits or 429 responses.

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
