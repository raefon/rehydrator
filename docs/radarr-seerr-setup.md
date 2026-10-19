# Radarr and Seerr Setup

## Radarr

Radarr provides imported movie paths, TMDb IDs, and download history.

Recommended Radarr Connect webhook URL:

```text
http://tenet-rehydrator:8080/api/radarr/webhook?token=<REHYDRATOR_API_TOKEN>
```

Enable these Radarr events:

```text
On Movie Added
On Download/Import
On Upgrade
```

The webhook lets Rehydrator seed or update rows immediately instead of waiting for polling.

## Radarr naming

For new imports, use stable filenames with TMDb IDs. Example folder format:

```text
{Movie CleanTitle} ({Release Year}) [tmdb-{TmdbId}]
```

Example file format:

```text
{Movie CleanTitle} ({Release Year}) [tmdb-{TmdbId}] - {Quality Full}-{Release Group}
```

Avoid bulk-renaming a large existing library until the system is stable, because path churn can trigger Jellyfin and CSI-rclone noise.

## Seerr

Seerr request sync/webhooks can create placeholder rows before Radarr import completes.

Recommended webhook URL:

```text
http://tenet-rehydrator:8080/api/seerr/webhook?token=<REHYDRATOR_API_TOKEN>
```

Seerr test notifications return HTTP 200 after authentication without creating tracking rows or triggering rearming, so the webhook can be tested and saved. Include `"notification_type": "{{notification_type}}"` in the JSON payload. Real movie notifications still require a TMDB or Arr ID.

Seerr sync config:

```yaml
seerr:
  sync:
    enabled: true
    interval_seconds: 300
    limit: 100
```

## Placeholder promotion

When Seerr sees a new request, Rehydrator can create a temporary row. When Radarr imports the movie, Rehydrator promotes that row to the real Radarr movie ID.
