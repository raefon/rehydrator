# Rehydrator v0.6.12 — Opt-in archived import recovery (unreleased)

## Added
- `file_health.recover_archived` (default false) brings current Arr-owned archived torrent imports with validated matching saved sources back into provider health reconciliation when pruning is disabled.
- Transition to unrequested BROKEN, not AVAILABLE or an immediate provider add. Existing health batch limits, fresh confirmations, per-release deduplication, recovery caps and cooldowns govern restoration.
- Skip source-less, mismatched/invalid sources, Usenet and in-flight rows. Retain metadata and write an audit event per transition. No new migration required.

# Rehydrator v0.6.11 — Seerr TV recovery and individual Arr missing files (unreleased)

## Added
- Accept TV requests through Seerr webhooks and polling, including Moonfin requests forwarded through Seerr. Only approved requests with explicit TVDB identity and season scope rearm matching saved sources. One primary submission per release; siblings await visibility.
- Deduplicate TV requests across webhook/polling using the Seerr request ID. Pending approvals, available content, unrequested seasons and imports without saved sources are not rearmed.
- Opt-in `jellyfin.inventory_sync.mark_missing_in_arr` updates individual Arr imported-file records for source-less failed torrents with confirmed dangling symlinks. Requires a readable same-mount canary, fresh Arr ownership/path checks, and rechecks absence immediately before the file-delete API.
- Keep acquisition with Seerr/Sonarr/Radarr. No automatic replacement search, unmonitoring or blocklisting. Default behavior preserves Arr files; no schema migration required.

# Rehydrator v0.6.10 — Flag missing imports without recovery sources (unreleased)

## Fixed
- Jellyfin inventory reconciliation checks path metadata for source-less torrent imports missing from Jellyfin. A confirmed missing path moves the unchanged idle import to FAILED with an explicit source-recovery-required reason, rather than repeatedly submitting impossible targeted scans.
- Valid saved sources and concurrent source/lifecycle changes are preserved. Existing files remain scan candidates; absence from Jellyfin alone does not establish file loss.
- Metadata checks are bounded and never open/read media bytes. Usenet imports bypass these checks. No migration, re-download, provider deletion or automatic Arr blocklisting is introduced.

# Rehydrator v0.6.9 — Reconcile missing Jellyfin movies and episodes (unreleased)

## Added
- Periodic metadata-only comparison of the synchronized Arr import inventory with Jellyfin movie/episode paths, including alternate versions and optional directory-prefix mappings.
- Queue at most five targeted scans per default 30-minute pass for AVAILABLE imports missing from the index, including older and never-played files without saved rearm sources.
- Durable per-item six-hour scan-attempt cooldowns across replicas/restarts, refresh audit entries, and pass summary logs. Incomplete/failed Jellyfin inventories abort the pass.
- Migration `030_jellyfin_inventory.sql` and YAML/environment controls. Requires targeted_scan mode and its Jellyfin plugin. No direct Usenet read probes, provider re-adds, lifecycle resets or full-library scan fallback.

# Rehydrator v0.6.8 — Torrent-only scheduled health checks (unreleased)

## Changed
- Exclude Usenet/NZB rows from scheduled file-health selection in filesystem-only and TorBox provider-first modes, avoiding background NNTP reads from routine scans.
- Retain Usenet playback recovery and rearm/visibility readability checks. Existing queued jobs are unchanged; proactive Usenet file-health recovery is disabled.
- Log the torrent-only source scope at worker startup. No migration or configuration change is required.

# Rehydrator v0.6.7 — Retry missing provider placements (unreleased)

## Fixed
- Waiting torrent releases absent from TorBox can retry their exact saved source through Decypharr after a 15-minute observation grace period.
- Migration `029_provider_retries.sql` reserves retries durably per tenant/hash before submission. At most three retries use 15/30/60-minute backoff; restarts and sibling episodes share the budget.
- Active or unknown Decypharr queue states, provider API errors, unknown provider results and invalid saved sources do not trigger resubmission. Queue cleanup preserves files. Waiting episodes continue normal visibility and Jellyfin refresh handling.

# Rehydrator v0.6.6 — Recover abandoned pruning (unreleased)

## Fixed
- With pruning disabled and rearm, file health and TorBox health enabled, current Arr inventory returns eligible PRUNING torrents to BROKEN after a durable 15-minute observation window. Matching saved sources are required; the bounded provider health worker handles recovery. Migration `028_stale_pruning.sql` adds the observation marker.
- Preserve archives, source-less files, recent operations and in-flight requests. No provider deletion, immediate add or blind AVAILABLE reset occurs.

# Rehydrator v0.6.5 — Recover confirmed provider losses (unreleased)

## Fixed
- Allow repeatedly confirmed TorBox losses to enter the existing Decypharr rearm flow even when most sampled releases are missing. Cached observations do not add confirmations; API errors and unknown results reset them.
- Preserve per-pass recovery caps, shared release deduplication, 24-hour cooldowns, source validation and filesystem outage protection.

# Rehydrator v0.6.4 — Reconcile unrequested broken torrents (unreleased)

## Fixed
- Include unrequested BROKEN torrent rows in provider-first health sweeps when TorBox health is enabled, retaining source validation, confirmation thresholds, outage suppression and recovery caps.
- Queue one missing-release restore and route eligible broken siblings into shared visibility waiting. Already provider-present, readable broken files enter visibility confirmation without a provider add.
- Archived, failed, requested, pruning, rearming, source-less and already-requested rows remain excluded from new automatic work. No bulk lifecycle reset or schema migration is introduced.

# Rehydrator v0.6.3 — TorBox provider-first health (unreleased)

## Added
- Opt-in read-only TorBox account checks compare tracked torrent hashes with complete paginated account inventory and `download_present`. Missing entries, unavailable payloads, and unknown API results are stored separately from media/source records.
- Migration `027_provider_health.sql` retains timestamped per-release observations, confirmation counts and shared recovery cooldowns. GET `/api/health/torbox` reports stored observations.
- Group checks and recovery by torrent hash. One season-pack rearm is submitted through Decypharr; related AVAILABLE files wait for provider readiness and readable mount data.

## Changed
- With TorBox health enabled, mount-only failures do not resubmit provider-available torrents; unknown results do not imply eviction. All provider mutations still go through Decypharr. NNTP retains its existing health flow.

# Rehydrator v0.6.2 — Health-scan progress past slow files (unreleased)

## Fixed
- Continue a file-health sample after individual one-byte read timeouts, using a separate four-slot outstanding-read budget. Never launch duplicate concurrent probes for the same path.
- Record busy/skipped probes as deferred observations, not unreadable-file evidence; advance rolling scan order while preserving prior failure counts.
- Keep health reads separate from rearm probes and log checked, deferred, and timed-out counts. Existing outage guards and recovery limits remain in force.
- Avoid retrying an identical CSI path through the configured root alias.

# Rehydrator v0.6.1 — Automatic file-health recovery (unreleased)

## Added
- Rolling, bounded file-read probes for AVAILABLE media with saved sources: repeated unreadability queues the existing rearm flow without needing a Jellyfin playback event.
- Durable confirmation counts, per-tenant sweep coordination, at most two automatic queues per default pass, and a 24-hour per-file/source automatic recovery cooldown.
- Decypharr health checks, timeout protection, widespread-failure suppression, and an optional mount canary. Archived/in-flight items are excluded.
- Rearm completion and the optional visible-path shortcut now require actual readable bytes using bounded probes, rather than stale file metadata.
- Migration `026_file_health.sql`, configuration/env options, and availability-versus-retention guidance. Local pruning policy remains unchanged; no unconditional 30-day rearm timer.

# Rehydrator v0.6.0 — Usenet support and TorBox Pro NNTP (unreleased)

## Added
- Decypharr provider configuration fragment for TorBox Pro's NNTP news server, with a conservative shared connection budget and setup instructions.
- Document the selected routing: torrent cloud access and direct TorBox NNTP for NZBs, with credentials held in Decypharr and Usenet pruning disabled. Cloud Usenet cache/CDN routing is deferred.
- Capture and retain original NZB bytes from explicit Arr Usenet grabs, with import associations for movies and season-pack episodes.
- Rearm via Decypharr's SABnzbd-compatible API, preserving opaque runtime job IDs separately from torrent hashes and original import IDs.
- Share current NZB job mappings across season-pack episodes with database serialization; poll existing jobs instead of repeatedly submitting.
- Migration `025_usenet_sources.sql` adds protocol-aware source storage and runtime job mappings. Existing torrent sources remain valid.

## Changed
- Usenet is excluded from automatic pruning. Failed NZB jobs require recovery rather than automatic resubmission of missing articles.
- Usenet readiness requires a completed job or retained catalog identity plus a readable mounted file. Targeted Jellyfin notifications for known Usenet imports are gated on readability.

# Rehydrator v0.5.11 — Jellyfin Targeted Scans support (unreleased)

## Added
- Optional `jellyfin.refresh_mode: targeted_scan` calls the Targeted Scans plugin directly and validates Created/Refreshed results. No full-library fallback.

## Fixed
- Describe successful refresh requests as accepted rather than completed scans.

# Rehydrator v0.5.10 — Legacy lifecycle constraint repair (unreleased)

## Fixed
- Repair existing database state constraints during schema initialization so successful rearm submissions can enter `WAITING_FOR_VISIBILITY`. Standalone migration: `024_repair_lifecycle_state_constraint.sql`.

# Rehydrator v0.5.9 — Jellyfin playback user context (unreleased)

## Fixed
- Log incoming Jellyfin playback starts and lookup failures; ignored non-start notifications are logged at debug level.
- Pass the playback webhook user ID into Jellyfin item lookups to avoid empty-user GUID errors with API-key authentication.

# Rehydrator v0.5.8 — Jellyfin authentication header (unreleased)

## Fixed
- Authenticate Jellyfin item lookup, health checks, and library refreshes using `Authorization: MediaBrowser Token="…"`.

# Rehydrator v0.5.7 — Seerr webhook test support (unreleased)

## Fixed
- Accept authenticated Seerr test notifications without requiring a media ID or triggering rearming, allowing webhook settings to be saved.

# Rehydrator v0.5.6 — Jellyfin and durable Decypharr rearming (unreleased)

## Changed
- Jellyfin is the sole media-server integration. Remove the old media-server config/env settings and use `/api/playback/jellyfin` and `/api/jellyfin/*` endpoints. Targeted refreshes, independent refresh auditing, dedupe, metrics, dependency checks, and self-heal now use Jellyfin.
- Decypharr owns rearm, provider deletion, and status checks. TorBox credentials belong inside Decypharr; Rehydrator no longer calls TorBox directly.
- Routine rearm submits one explicitly typed source: original `.torrent` bytes or the original magnet. Infohashes remain identity only; mixed sources and implicit hash-to-magnet conversion are rejected. Missing sources are recovered from Arr history or explicitly flagged for recovery.
- Arr grab webhooks durably capture the original torrent file or resolved magnet before import; import notifications attach the exact download ID to media rows. Import sync remains a fallback. Metainfo is retained in PostgreSQL and uploaded using Decypharr's multipart `torrents` field.
- Rearm checks Decypharr completion plus mount visibility. If Arr has removed the completed queue entry, an exact retained-library hash and readable mounted file can establish readiness. Errors do not resubmit accepted downloads.
- Prune preserves source metadata first, requests provider deletion using Decypharr's browse API, and clears the old queue entry with `deleteFiles=false` to preserve local links. Provider removal is asynchronous inside Decypharr; acceptance does not independently confirm TorBox deletion.

## Added
- Migration `023_jellyfin_refresh_audit.sql` for independent Jellyfin refresh history; historical migration files and audit data are retained.
- Migration `022_explicit_torrent_source_kinds.sql` and database constraints for exclusive source payloads. Legacy files and extended magnets are classified; bare generated/ambiguous magnets and hash-only rows require source recovery. Newly captured genuine bare magnets remain valid.
- Migration `021_grab_source_capture.sql` for durable pre-import sources, retry state, and exact import associations, including season-pack episode files. Newer pending imports cannot reuse older release identities.
- Migration `020_saved_torrent_sources.sql` and matching automatic schema initialization for original torrent bytes, filenames, and hashes.
- Decypharr API-token authentication (`decypharr.api_token` / `DECYPHARR_API_TOKEN`).
- Infohash verification of stored/uploaded metainfo to reject mismatched files and invalid indexer responses.
- Jellyfin playback webhooks and automatic library notifications; setup in `docs/jellyfin-setup.md`.
- Regression coverage for exclusive source submission, magnet redirects, preserved links during prune, readiness checks, source integrity, and PostgreSQL byte-for-byte persistence.

## Fixed
- Prowlarr redirects to magnets are submitted to Decypharr without attempting to download the magnet over HTTP. Download fetch errors omit full URLs containing indexer credentials.
- Preserve full magnets, including trackers, when resolving Arr history.

## Upgrade notes
- Apply migrations 020–023 or enable `db_auto_migrate` before starting this revision. Enable On Grab and On Import (including upgrades) on both Arr webhook connections. Remove Completed can remain enabled.
- Set `REARM_BACKEND=decypharr` if using an earlier unreleased native-backend configuration. Provider pruning requires a Decypharr version exposing `DELETE /api/browse/torrents/{hash}` and an API token when authentication is enabled.
- Existing database rows cannot reconstruct a lost original torrent file from its hash alone. New imports capture their source while the original download URL is still accessible.
- The legacy `delete_files_on_prune` setting no longer controls provider pruning: local link deletion is always disabled.
- See `docs/decypharr-rearm.md` for limits and live validation.

---

# Build and release automation (unreleased)

## Added
- Optional Jellyfin integration with automatic path refreshes, authenticated playback webhooks, and movie/episode matching through Jellyfin item lookup. See `docs/jellyfin-setup.md`.
- Run Go tests and compile checks on pushes to `main` and pull requests targeting `main`.
- On version-tag pushes (`v*.*.*`), build Linux AMD64, Linux ARM64, and macOS ARM64 binaries and generate SHA-256 checksums.
- Publish multi-architecture (`linux/amd64`, `linux/arm64`) images to GHCR with versioned tags and `latest`.
- Automatically create the GitHub Release from a pushed version tag, or replace its binary assets on a workflow rerun if the release already exists.
- Scope write permissions to the publish job; normal commit and pull-request builds use read-only permissions.

## Fixed
- Handle Prowlarr download URLs that redirect to magnet URIs by submitting the resolved magnet to Decypharr, preserving its trackers. Keep torrent-file downloads working and omit full download URLs from fetch-error logs.

## Notes
- The Docker build defaults to version `dev`; release workflows pass the version parsed from the Git tag.
- Retrying a failed workflow for an existing tag should be done with GitHub Actions' **Re-run jobs**. Pushing an unchanged tag does not itself guarantee a new workflow run.

---

# Rehydrator v0.4.8 — Version reporting

## Added
- Print the embedded version during application startup, making the actual running build visible in pod logs.
- Add `--version` / `-version` CLI flag to print the version and exit before loading configuration or connecting to dependencies.
- Add Docker build argument `VERSION` and inject it into the binary using Go linker flags.
- Update the Makefile build target to embed the version from `VERSION`.

## Validation
- Go source formatted with `gofmt`.
- Full Go tests/build not run in this environment because dependency availability is not verified.

---

# Rehydrator v0.4.7 — Webhook diagnostics

## Added
- Structured `INFO` logs for authenticated Radarr and Sonarr webhook receipt, including event type, Arr ID when present, title when present, and remote address.
- Structured logs for Radarr/Sonarr refresh completion, refresh failures, debounce skips, and successful deletion reconciliation.
- `WARN` logs for webhook requests rejected due to unauthorized access, unsupported methods, or invalid JSON.
- Explicit `Test` event acknowledgement (`202 Accepted`, `action: test_acknowledged`) for both Arr webhook endpoints. Test events no longer trigger a library refresh.
- Processing outcome logs for Sonarr webhook events, including match status and whether refresh was performed or debounced.

## Security and behavior
- Logs intentionally exclude authorization headers, tokens, query strings, and complete webhook payloads.
- Existing deletion reconciliation, tombstones, and periodic inventory reconciliation behavior are retained from v0.4.6.

## Validation
- `gofmt` completed for the modified Go source.
- `go test ./internal/health` could not run because the checkout is missing required `go.sum` entries for `github.com/jackc/pgx/v5`; dependencies were not fetched in this environment. Tests are therefore **not verified**.

---

# Rehydrator v0.4.6 — Arr deletion reconciliation

## Added
- Immediate Radarr `MovieDelete` / `MovieDeleted` webhook handling.
- Immediate Sonarr `SeriesDelete` / `SeriesDeleted` webhook handling.
- Periodic full-inventory reconciliation for stale Radarr movie rows and Sonarr episode-file rows.
- `media_cache_tombstones` audit table retaining Arr source, IDs, title/external ID, reason, and deletion time.
- Tracking for Radarr movies present in Arr but not yet associated with an imported file, without queuing downloads.
- Migration `020_arr_deletion_reconciliation.sql` and webhook/reconciliation documentation.

## Safety
- Reconciliation runs only after successful full inventory retrieval.
- Sonarr pruning is skipped if any per-series episode or episode-file inventory request fails.
- Requested placeholders are retained.
- No media files or provider-side torrents are deleted by this feature.
- Re-adding an item to Arr allows normal sync to recreate active tracking; this change does not force a download.

## Validation note
`gofmt` completed. `go test ./...` could not run in the build environment because required Go modules were not cached and outbound DNS/network access to `proxy.golang.org` was unavailable. Run `go test ./...` in the normal build environment before deployment.
