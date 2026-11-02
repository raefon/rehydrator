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

## v0.6.0: Usenet sources

Apply `025_usenet_sources.sql` after prior migrations (or use startup schema
initialization). It adds explicit protocols, original NZB bytes/filenames, opaque
runtime job IDs, and a shared season-pack job mapping. Existing torrent payloads
remain separate. Usenet is excluded from automatic pruning. See
[Hosted-NZB setup](nzb-gateway-setup.md).

## v0.6.1: File-health observations

Migration `026_file_health.sql` adds persistent per-file failure observations,
automatic-recovery cooldown timestamps, and per-tenant sweep timestamps. It does
not rearm or change existing lifecycle states when applied. Startup schema
initialization includes it. See [file-health recovery](file-health.md).

## v0.6.3: Provider observations

`027_provider_health.sql` adds per-tenant/provider/hash observations, confirmation
counts, and shared recovery cooldown timestamps. Applying it does not reconcile
or delete media. The old provider sweep has been superseded by gateway polling. See
historical provider observations.

## v0.7.0: Direct gateway

`031_cloud_usenet.sql` creates the original cloud-NZB queue and submission gate.
`032_direct_gateway.sql` adds retained protocol-specific sources, the shared
torrent/NZB job catalog, and expiring hashed client sessions. It copies existing
cloud-NZB jobs without changing their IDs, retaining the old table. Neither
migration submits downloads, deletes library data, nor changes CSI mounts.
Apply both in order after earlier migrations, or enable `db_auto_migrate`.
Back up original-source payloads as credentials may occur in private source data.
See [direct gateway setup](torbox-gateway-setup.md) for the fresh playground.

## v0.7.1: Gateway/controller handoff

`033_gateway_roles.sql` adds an independent mount-verification lease, catalog
revision and deferred staging cleanup to gateway jobs. Apply it after 032, before
starting either new role. Run migrations once before controllers start (or let
the gateway run `db_auto_migrate: true` first); controllers should use false.
It does not submit provider jobs or modify library paths.

## v0.7.9: Provider telemetry

`034_provider_status.sql` adds nullable provider observations to gateway jobs.
Apply it before rolling out new gateway/controller binaries.

## v0.8.0: Gateway-only consolidation

No new migration is required beyond 034. Old tables and historical migrations are
retained so existing databases and source payloads can still be upgraded. Update
removed YAML/environment settings using [configuration](configuration.md); do not
drop tables or delete old source records as part of this upgrade.

## v0.8.1: SAB release folders

`035_sab_release_folder.sql` adds a persisted release folder for NZB import staging.
Visible completed NZBs without a pinned folder return to Mounting for local
verification; their provider IDs, source payloads, attempts and WebDAV paths stay
unchanged. The controller creates the release-named staging links and verifies a
bounded read before completion. Existing old links remain in place. Hidden history
is not restaged until made visible again. Apply 035 before upgrading either role.
