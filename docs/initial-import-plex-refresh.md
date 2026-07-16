# Initial import Plex refresh

v0.4.3 adds targeted Plex refreshes for first imports and path changes. Earlier
versions refreshed Plex after rehydration and self-heal, but an item first
imported by Radarr/Sonarr could stay invisible until a manual Plex scan.

## Behavior

Radarr seed sync and Sonarr episode-file seed sync now compare the imported item
against the existing Rehydrator row before upserting it.

A targeted Plex refresh is queued only when one of these changes is detected:

- no existing row exists and the imported path is now known
- an existing `REQUESTED` row becomes `AVAILABLE`
- an existing row had an empty `symlink_path` and now has a real path
- an imported path changed because of an upgrade or rename

Normal steady-state seed loops do not queue Plex refreshes.

The refresh goes through the same Plex dedupe/cooldown path introduced in
v0.3.4, so webhook bursts and seed loops should not repeatedly scan the same
folder. Dedupe is target-path based, so multiple new episode files in the same
season folder collapse into one Plex scan.

## Config

```yaml
plex:
  enabled: true
  movie_section_id: 1
  tv_section_id: 2
  refresh_after_import: true
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_dedupe_minutes: 10
```

Environment override:

```bash
PLEX_REFRESH_AFTER_IMPORT=true
```

## Expected logs

Movie import/promotion:

```text
radarr imported item queued for Plex refresh reason=initial_import_available
Plex refresh queued action=radarr_initial_import_available
```

Series episode-file import/promotion:

```text
sonarr imported episode file queued for Plex refresh reason=initial_import_available
Plex refresh queued action=sonarr_initial_import_available
```

For series, the target path is the episode file's parent folder, usually the
season folder.

## Debug

```bash
curl -s -H "Authorization: Bearer $API_TOKEN" \
  "http://localhost:8080/api/plex/refreshes/recent?limit=20" | jq
```

```sql
SELECT arr_id, action, scope, path, status, message, created_at
FROM media_cache_plex_refreshes
ORDER BY created_at DESC
LIMIT 20;
```
