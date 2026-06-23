# Plex playback re-arm

This release adds a Plex Pass native webhook endpoint for playback-intent re-arm.

## Endpoint

Configure Plex Webhooks to post to:

```text
http://tenet-rehydrator.tenet-nofear101.svc.cluster.local:8080/api/playback/plex?token=<API_TOKEN>
```

If you expose Rehydrator behind ingress, use the external HTTPS URL instead.

The endpoint accepts Plex's native multipart webhook payload field named `payload` and also accepts raw JSON for easier testing.

## Behavior

When Plex sends a playback-start style event:

- If the tracked movie is `AVAILABLE`, Rehydrator records the intent and does nothing.
- If the tracked movie is `ARCHIVED`, `BROKEN`, `FAILED`, or `REQUESTED`, Rehydrator sets `rearm_requested=true` and the existing Decypharr re-arm worker handles the restore.
- Cooldown prevents repeated play clicks from spamming Decypharr.

## Test with JSON

```bash
curl -i -X POST \
  "http://localhost:8080/api/playback/plex?token=$API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "event": "media.play",
    "Metadata": {
      "type": "movie",
      "title": "Attack on Titan: Wings of Freedom",
      "Guid": [{"id":"tmdb://330081"}]
    },
    "Account": {"title":"Kyle"}
  }'
```

## Generic endpoint

For dashboards, scripts, or future Jellyfin/Tautulli integration, use:

```bash
curl -i -X POST \
  "http://localhost:8080/api/playback/event" \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source": "manual-test",
    "event": "media.play",
    "media_type": "movie",
    "tmdb_id": 330081,
    "title": "Attack on Titan: Wings of Freedom"
  }'
```

## Plex pre-roll idea

Plex's movie pre-roll can point at a short local MP4 that tells users the cache may be waking up. This is optional but useful because archived items may need 30-120 seconds before the second play attempt succeeds.

Create a small MP4 with `scripts/create-rehydrator-preroll.sh`, mount it somewhere Plex can read, and add that file path to Plex's cinema trailer/pre-roll setting.

## Pre-roll webhook filtering in v0.2.8

Plex sends a separate `media.play` webhook for the pre-roll MP4 itself. If your pre-roll file is named `rehydrator-preroll.mp4`, keep the default ignore settings:

```yaml
playback:
  ignored_titles:
    - rehydrator-preroll
  ignored_title_contains:
    - preroll
    - pre-roll
```

This prevents the pre-roll from creating unmatched playback intent rows or triggering an unnecessary Radarr refresh.
