# Sonarr / series support (v0.4.1)

v0.4.1 fixes and extends the first Sonarr MVP. It tracks one Rehydrator row per imported Sonarr episode file, using Sonarr episode-file rows as the source of truth.

## Model

For movies:

```text
media_type = movie
arr_id     = Radarr movie id
```

For Sonarr:

```text
media_type = series
arr_id     = Sonarr episodeFileId
```

Additional Sonarr metadata is stored on the same row:

```text
series_id
episode_id
episode_file_id
season_number
episode_number
series_title
episode_title
cache_group_key
```

This intentionally avoids using the Sonarr series ID as the cache lifecycle ID. A show can have many imported episode files, and each file can be pruned/rearmed independently.

## Config

```yaml
sonarr:
  url: http://tenet-sonarr.tenet-nofear101.svc.cluster.local:8989
  api_key: "<SONARR_API_KEY>"

sonarr_sync:
  enabled: true
  interval_seconds: 60

decypharr:
  sonarr_category: sonarr

plex:
  tv_section_id: 0 # optional; leave 0 to auto-discover the first TV library
```

## Flow

```text
Sonarr /api/v3/episodeFile?seriesId=<id> imported episode file
→ sonarr_sync seeds media_cache_state row
→ row becomes AVAILABLE
→ metadata resolution tries to match Sonarr history and save infohash/magnet
→ prune removes the TorBox torrent by infohash after cache_grace
→ playback/manual rearm queues Decypharr with category=sonarr
→ CSI visibility marks AVAILABLE again
→ Plex refresh targets the season folder / episode parent folder
```

## v0.4.1 seed behavior

The syncer now uses `/api/v3/episodeFile?seriesId=<id>` for imported files. `/api/v3/episode?seriesId=<id>` is still read for labels such as episode title, season number, and episode number, but it is no longer used to decide whether a file exists.

Webhook-triggered Sonarr refreshes are debounced so import/rename bursts do not launch many overlapping full seed syncs. Overlapping sync attempts are skipped and logged.

## Endpoints

```bash
POST /api/refresh/sonarr
POST /api/rearm/series/<episodeFileId>
POST /api/prune/series/<episodeFileId>
GET  /api/state/series/<episodeFileId>
POST /api/sonarr/webhook
POST /api/plex/refresh/series/<episodeFileId>
POST /api/plex/refresh/tv
GET  /api/admin/sonarr/status
GET  /api/admin/sonarr/episode-files?limit=100
```

## Plex playback matching

Movie playback can still match by TMDb ID. Series playback primarily matches by exact file path from the Plex webhook payload. If Plex does not include the file path in the webhook payload, use the generic playback endpoint with `media_type=series` and `path` or `arr_id=<episodeFileId>`.

Example:

```bash
curl -i -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  http://localhost:8080/api/playback/event \
  -d '{
    "source": "manual-test",
    "event": "media.play",
    "media_type": "series",
    "arr_id": 12345
  }'
```

## Season packs

This MVP uses one row per episode file. Multi-episode files and season packs are represented by the episodeFileId row using the first episode label Sonarr returns for that file. Later work should add shared infohash/cache group handling for season-pack aware rearm/prune decisions.


## v0.4.2 first-import refresh

Sonarr episode-file seed sync now queues a targeted Plex season-folder refresh when an episode file is first discovered, when a placeholder/requested row becomes available, or when an episode file path changes. Normal steady-state syncs do not queue refreshes.
