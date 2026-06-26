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
