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
