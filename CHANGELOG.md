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
