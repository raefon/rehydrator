# v0.4.3 Plex refresh dedupe SQL fix

## Problem

`v0.4.2` added initial-import Plex refreshes and target-path dedupe. During bulk Sonarr imports, PostgreSQL could log:

```text
Plex refresh dedupe DB check failed; continuing
ERROR: could not determine data type of parameter $2 (SQLSTATE 42P18)
```

The refresh still queued because Rehydrator intentionally continued after the dedupe check failed, but the warning was noisy and meant the database-backed dedupe check was not reliable for that refresh attempt.

## Cause

The dedupe query accepted an unused `mediaID` parameter and referenced later placeholders. PostgreSQL could not infer the unused parameter type for `$2` in the prepared/extended query path.

## Fix

`v0.4.3` removes the unused parameter and passes a concrete timestamp cutoff from Go:

```sql
SELECT EXISTS (
    SELECT 1
    FROM media_cache_plex_refreshes
    WHERE tenant = $1
      AND path = NULLIF($2, '')
      AND status = 'success'
      AND created_at > $3::timestamptz
)
```

The in-memory dedupe still prevents duplicate queued refreshes during a single run. The database check now reliably prevents duplicate refreshes after restarts or when recent successful refreshes already exist.

## Validation

Watch for the absence of the old warning:

```bash
kubectl logs -n tenet-nofear101 deploy/tenet-rehydrator --since=20m | grep -Ei \
'Plex refresh dedupe DB check failed|Plex refresh skipped by dedupe|Plex refresh queued|Plex refresh complete'
```

Expected behavior during a season-pack import:

```text
sonarr imported episode file queued for Plex refresh
Plex refresh queued action=sonarr_initial_import_available target_path=/storage/media/tv/<Series>/Season <N>
Plex refresh skipped by dedupe ... target_path=/storage/media/tv/<Series>/Season <N>
Plex refresh complete
```
