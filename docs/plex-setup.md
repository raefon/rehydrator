# Plex Setup

Plex should keep library entries even when backing TorBox/CSI-rclone files are temporarily unavailable.

## Required Plex settings

In Plex server settings, keep these off:

```text
Empty trash automatically after every scan: Off
Allow media deletion: Off
Scan my library automatically: Off or cautious
Run a partial scan when changes are detected: Off or cautious
Generate video preview thumbnails: Never
Generate chapter thumbnails: Never
```

These settings avoid deleting or bloating the Plex library when Rehydrator prunes provider cache.

## Playback webhook

Set Plex Webhook URL to:

```text
http://tenet-rehydrator:8080/api/playback/plex?token=<REHYDRATOR_API_TOKEN>
```

If Plex is in a different namespace, use the fully qualified service name:

```text
http://tenet-rehydrator.tenet-nofear101.svc.cluster.local:8080/api/playback/plex?token=<REHYDRATOR_API_TOKEN>
```

## Pre-roll

The pre-roll is optional and only provides a user hint. It is not the re-arm trigger.

Suggested message:

```text
Rehydrating media cache. If playback fails, wait a minute and press Play again.
```

Make sure the pre-roll is configured on the same Plex server that owns the active library.

Configure ignored playback titles so the pre-roll itself is not treated as an unmatched movie:

```yaml
playback:
  ignored_titles:
    - rehydrator-preroll
  ignored_title_contains:
    - preroll
    - pre-roll
```

## Plex hygiene refresh

Enable Plex refresh after rehydration:

```yaml
plex:
  enabled: true
  url: http://tenet-plex.tenet-nofear101.svc.cluster.local:32400
  token: "<PLEX_TOKEN>"
  movie_section_id: 0
  refresh_after_rearm: true
  refresh_after_visibility: true
  refresh_after_prune: false
  refresh_delay_seconds: 45
```

Keep `refresh_after_prune` off so Plex does not eagerly mark archived paths as missing.
