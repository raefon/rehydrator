# Radarr webhook seeding

Use Radarr Connect to notify Rehydrator as soon as Radarr adds or imports a movie. This avoids waiting for the periodic Radarr poller and makes Plex playback intent tracking more reliable.

## URL

```text
http://tenet-rehydrator:8080/api/radarr/webhook?token=YOUR_API_TOKEN
```

If Radarr is in another namespace, use the fully-qualified service DNS name:

```text
http://tenet-rehydrator.tenet-nofear101.svc.cluster.local:8080/api/radarr/webhook?token=YOUR_API_TOKEN
```

## Enable these Radarr events

```text
On Movie Added
On Download/Import
On Upgrade
```

`On Rename` is optional.

## What Rehydrator does

1. Parses the Radarr payload for `movie.id`, `movie.tmdbId`, `movie.title`, and optional `movieFile.path`.
2. Creates or updates a REQUESTED row when Radarr adds the movie before it is imported.
3. Promotes any Seerr-created placeholder row to the real Radarr movie ID.
4. Runs an immediate Radarr refresh so imported movies are seeded without waiting for the poll interval.
5. Records a `radarr_webhook_received` and `radarr_webhook_seeded` event when a row matches.

## Test

```bash
curl -i -X POST "http://localhost:8080/api/radarr/webhook?token=$API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "eventType": "MovieAdded",
    "movie": {
      "id": 123,
      "title": "Example Movie",
      "tmdbId": 999999
    }
  }'
```

Expected response:

```json
{"ok":true,"matched":true,"refresh":true,"arr_id":123,"tmdb_id":999999}
```
