# Availability and mount checks

The gateway polls torrent and hosted-NZB placements in TorBox. Confirmed missing
placements use their retained original source through the normal bounded recovery
queue. See [gateway setup](torbox-gateway-setup.md) and the dashboard's Recovery &
diagnostics page. The older independent periodic file/provider sweep is removed.

Provider metadata alone does not authorize completion: the controller creates
staging links and verifies a bounded read from the shared mount. This avoids
reading the entire library or downloading complete files for a health check.

Jellyfin inventory checks can identify missing imported paths. Opt-in Arr
missing-file reporting requires a known-readable file on the same remote mount:

```yaml
file_health:
  canary_path: /storage/torbox/torrents/YOUR-STABLE-JOB/path/to/readable-file.mkv
jellyfin:
  inventory_sync:
    mark_missing_in_arr: true
```

Use an existing readable media file, not a local empty sentinel. If the canary
cannot be read, Arr file-record updates pause. This protects against mistaking a
mount outage for many missing releases. There is no provider keepalive or promise
that local checks extend TorBox retention.
