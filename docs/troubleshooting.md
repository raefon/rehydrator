# Troubleshooting

## CSI path did not appear quickly

If logs show:

```text
CSI path did not appear within ...
```

The provider add may have succeeded, but CSI-rclone has not refreshed yet. With v0.2.9+ this should move to `WAITING_FOR_VISIBILITY` and retry visibility checks.

Check state:

```sql
SELECT arr_id, arr_title, state, last_error, updated_at
FROM media_cache_state
WHERE tenant = 'tenet-nofear101'
ORDER BY updated_at DESC;
```

## TorBox/WebDAV 429

If CSI-rclone logs show:

```text
HTTP 429 Too Many Requests
```

Reduce readers and concurrency:

```yaml
concurrent_workers: 2
max_rearms_per_run: 3
max_prunes_per_run: 5
```

Keep Plex heavy analysis off:

```text
Video preview thumbnails: Never
Chapter thumbnails: Never
Extensive analysis: Off
```

## Plex shows trash/unavailable icon

This means Plex has noticed the backing path is unavailable. Do not enable automatic trash emptying.

After rehydration, Rehydrator can refresh Plex:

```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/plex/refresh/movie/4
```

## Pre-roll creates unmatched playback events

Make sure ignored titles are configured:

```yaml
playback:
  ignored_titles:
    - rehydrator-preroll
  ignored_title_contains:
    - preroll
    - pre-roll
```

## Movie does not re-arm on play

Check:

1. Plex webhook URL includes the correct token.
2. Rehydrator has a row for the movie.
3. TMDb ID is present.
4. State is `ARCHIVED`.
5. Cooldown is not suppressing repeat playback events.

## Useful SQL

State summary:

```sql
SELECT state, count(*)
FROM media_cache_state
WHERE tenant = 'tenet-nofear101'
GROUP BY state
ORDER BY state;
```

Recent errors:

```sql
SELECT arr_id, arr_title, state, last_error, updated_at
FROM media_cache_state
WHERE tenant = 'tenet-nofear101'
  AND last_error IS NOT NULL
  AND last_error <> ''
ORDER BY updated_at DESC
LIMIT 20;
```
