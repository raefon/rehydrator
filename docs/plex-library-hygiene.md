# Plex Library Hygiene

Rehydrator keeps Plex library items visible while TorBox cache is pruned. Plex may show a trash/unavailable icon when the backing file path is temporarily missing. The file can still be re-armed by playback webhook, but Plex may need a library refresh after the file becomes visible again.

v0.3.0 adds Plex refresh support:

- After a successful re-arm, Rehydrator can ask Plex to refresh the movie path.
- After `WAITING_FOR_VISIBILITY` becomes `AVAILABLE`, Rehydrator can refresh the movie path.
- Rehydrator does **not** refresh Plex after prune by default because that makes Plex notice archived/missing files sooner.
- Manual API endpoints can refresh a single movie or the whole movie section.

## Config

```yaml
plex:
  enabled: true
  url: http://tenet-plex.tenet-nofear101.svc.cluster.local:32400
  token: "<PLEX_TOKEN>"
  movie_section_id: 0
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_after_prune: false
  refresh_delay_seconds: 45
  refresh_timeout_seconds: 20
  max_refreshes_per_run: 5
```

`movie_section_id: 0` lets Rehydrator auto-discover the first Plex movie library. Set the actual section ID if you have multiple movie libraries.

## API

Refresh one tracked movie:

```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/plex/refresh/movie/4
```

Refresh the whole movie section:

```bash
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/plex/refresh/movies
```

## Metrics

```text
rehydrator_plex_refresh_total
rehydrator_plex_refresh_failures_total
```

## Recommended Plex settings

Keep these off:

- Empty trash automatically after every scan
- Allow media deletion
- Video preview thumbnails
- Automatic library scan
- Partial scan on changes

Keep Cinema Trailers enabled only if you use the Rehydrator pre-roll.

## Targeted refreshes

Rehydrator should prefer targeted Plex scans. A movie refresh uses the parent folder of the movie file path and calls Plex's section refresh endpoint with a `path` query parameter. This avoids scanning the entire movie library just to clear the unavailable/trash icon for one restored title.

Example target path:

```text
/storage/media/movies/Chainsaw Man - The Movie - Reze Arc (2025)
```

Whole-section refreshes are still available through the manual API endpoint, but they should be treated as repair tools rather than the normal workflow.

Recommended behavior:

```text
After prune: no Plex refresh
After rearm/visibility: targeted movie-folder refresh
Manual repair: targeted movie refresh first, whole-section refresh only if needed
```

## Self-healing refresh worker

v0.3.2 also includes the self-healing behavior originally planned for v0.3.3. It periodically looks for recently rehydrated or recently played `AVAILABLE` movies that do not yet have a successful Plex refresh audit after that event.

```yaml
self_heal:
  enabled: true
  interval_seconds: 300
  plex_refresh_available: true
  plex_recent_hours: 24
  max_plex_refreshes_per_run: 5
```

This is intentionally conservative. It only queues targeted movie-folder refreshes, and it does not refresh after prune.
