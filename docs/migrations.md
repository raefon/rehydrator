# Migrations

Use `db_auto_migrate: true` to initialize or update the current schema. For manual
upgrades, apply the applicable SQL files in `migrations/` in order, including:

- `020_saved_torrent_sources.sql` — preserved original source payloads.
- `020_arr_deletion_reconciliation.sql` — Arr inventory deletion audit.
- `021_grab_source_capture.sql` — pre-import capture jobs and exact import associations.
- `022_explicit_torrent_source_kinds.sql` — exclusive source kinds and legacy recovery.
- `023_jellyfin_refresh_audit.sql` — independent Jellyfin refresh history, dedupe, metrics, and self-heal.

Historical migrations remain unchanged for existing installations. Their old
media-server audit tables are retained as historical data and are not queried by
the current application. Jellyfin refresh history starts in its own table, so
historical scans cannot suppress Jellyfin refreshes.

No migration is applied to your running database by editing the repository.
