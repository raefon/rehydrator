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
