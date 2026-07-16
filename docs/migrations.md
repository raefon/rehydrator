# Migrations

Migrations live in `migrations/` and can be run manually or applied automatically when `db_auto_migrate: true` is enabled.

## Manual run

```bash
psql "$POSTGRES_URL" -f migrations/001_init.sql
psql "$POSTGRES_URL" -f migrations/002_decypharr.sql
psql "$POSTGRES_URL" -f migrations/003_torbox_prune.sql
psql "$POSTGRES_URL" -f migrations/004_v9_behavior_notes.sql
psql "$POSTGRES_URL" -f migrations/005_radarr_sync_api.sql
psql "$POSTGRES_URL" -f migrations/006_seerr_sync.sql
psql "$POSTGRES_URL" -f migrations/007_hardening_api_metrics.sql
psql "$POSTGRES_URL" -f migrations/008_playback_intent.sql
psql "$POSTGRES_URL" -f migrations/009_event_driven_seed.sql
psql "$POSTGRES_URL" -f migrations/010_playback_ignore_preroll.sql
psql "$POSTGRES_URL" -f migrations/011_visibility_provider_cooldown.sql
psql "$POSTGRES_URL" -f migrations/012_plex_library_hygiene.sql
psql "$POSTGRES_URL" -f migrations/013_ops_self_heal.sql
psql "$POSTGRES_URL" -f migrations/014_metadata_guardrails.sql
psql "$POSTGRES_URL" -f migrations/015_plex_refresh_dedupe.sql
```

## Migration summary

| Migration | Purpose |
|---|---|
| `001_init.sql` | Initial media state/events schema |
| `002_decypharr.sql` | Decypharr metadata support |
| `003_torbox_prune.sql` | TorBox prune fields |
| `004_v9_behavior_notes.sql` | Behavior compatibility notes |
| `005_radarr_sync_api.sql` | Radarr sync/API fields |
| `006_seerr_sync.sql` | Seerr request tracking |
| `007_hardening_api_metrics.sql` | API and retry hardening |
| `008_playback_intent.sql` | Playback intent counters |
| `009_event_driven_seed.sql` | Placeholder and unmatched playback support |
| `010_playback_ignore_preroll.sql` | Pre-roll ignore audit |
| `011_visibility_provider_cooldown.sql` | WAITING_FOR_VISIBILITY and cooldowns |
| `012_plex_library_hygiene.sql` | Plex refresh audit |
| `013_ops_self_heal.sql` | Ops endpoints and self-heal indexes |
| `014_metadata_guardrails.sql` | Guardrails for placeholders and empty-path rows |
| `015_plex_refresh_dedupe.sql` | No-op notes for v0.3.4 refresh dedupe |

## 013_ops_self_heal.sql

Adds indexes for the v0.3.2 operational endpoints and the self-healing Plex refresh worker that was originally planned for v0.3.3.

Run after upgrading to the combined v0.3.2 package:

```bash
psql "$POSTGRES_URL" -f migrations/013_ops_self_heal.sql
```

## 014_metadata_guardrails.sql

Adds indexes and cleanup helpers for placeholder/empty-path safety. This prevents `REQUESTED` rows from entering re-arm, retry, or self-heal paths before Radarr import metadata exists.

## 015_plex_refresh_dedupe.sql

No schema change. v0.3.4 uses the existing `media_cache_plex_refreshes` table for dedupe/skipped audit rows and adds `GET /api/plex/refreshes/recent`.
