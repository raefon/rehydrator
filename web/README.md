# Gateway dashboard

The Go gateway serves the React/TypeScript app at `/`. Open the gateway's ingress
or its forwarded port. Controller-only processes do not expose the dashboard.

## Native Authentik OIDC (recommended)

Create an OAuth2/OpenID Provider and application in Authentik:

- Client type: confidential. Select a signing key for signed ID tokens.
- Redirect URI: strict match for `https://YOUR-GATEWAY-HOST/auth/callback`.
- Use the normal per-application issuer mode, e.g. `https://auth.example.com/application/o/rehydrator/`.
- Include the `openid`, `profile`, and `email` scope mappings. For group restrictions,
  include a mapping that emits a `groups` array in the ID token.
- Bind the application to the users/groups who should have access.

Configure the **gateway**, keeping the client secret in its Kubernetes Secret:

```yaml
gateway:
  dashboard_external_auth: false
  oidc:
    enabled: true
    issuer_url: https://auth.example.com/application/o/rehydrator/
    client_id: YOUR_CLIENT_ID
    client_secret: YOUR_CLIENT_SECRET
    redirect_url: https://YOUR-GATEWAY-HOST/auth/callback
    allowed_groups:
      - rehydrator-users
```

Issuer, client ID/secret, redirect URL and enabled flag also have `GATEWAY_OIDC_*`
environment overrides (see `.env.example`). Allowed groups are configured in YAML.
An empty allowlist accepts any authenticated user authorized by your IdP for this
application. Any matching listed group permits all dashboard operations for this
single tenant; these are not per-user permission roles.

Open the gateway URL: Rehydrator redirects to Authentik and uses your existing SSO
session. `/auth/login` starts login; `/auth/callback` handles the code; the dashboard
Sign out button POSTs to `/auth/logout`. Logout clears only the Rehydrator session,
not your global Authentik session. ForwardAuth is no longer required on dashboard
routes in OIDC mode; preserve routing to these endpoints if you retain it.

HTTPS is required for issuer, provider endpoints and the public callback. TLS can
terminate at the ingress; cookies are always Secure, HttpOnly, host-only and
SameSite=Lax. Keep the public Host header intact. No forwarded identity headers
or Basic credentials bypass native OIDC. The gateway must reach discovery, token,
and JWKS endpoints; failed startup discovery does not silently disable login.

Sessions and single-use login state are stored in bounded process memory; use one
gateway replica or sticky routing. Restarting loses sessions. Sessions expire at
the earlier of ID-token expiry or one hour. Group changes/revocation take effect
on the next sign-in or session expiry; no token refresh or back-channel logout is
implemented. OAuth tokens are not sent to the browser or retained in sessions.

For native OIDC tests, use the HTTPS gateway hostname. An HTTP localhost port
forward will not receive Secure cookies or match your registered callback URI.
Vite's local HTTP proxy is suitable for Basic/external-mode development instead.

Provider reference: [Authentik OAuth2/OIDC documentation](https://docs.goauthentik.io/add-secure-apps/providers/oauth2/).

## Existing Authentik / ForwardAuth

Keep your cluster's existing Authentik middleware on the gateway ingress. In the
**gateway** config enable:

```yaml
gateway:
  dashboard_external_auth: true
```

Alternatively set `GATEWAY_DASHBOARD_EXTERNAL_AUTH=true`. No second dashboard
login appears. This delegates authentication to the proxy; it is not native OIDC.
Protect `/` **and** `/dashboard/api/*` with ForwardAuth and prevent untrusted direct
access to the Service (network policy). External mode deliberately permits direct
requests, including port forwards, without login. Identity headers do not grant
access when external mode is off. This is one tenant per gateway, not per-user RBAC.

By default external auth is off and dashboard API requests require the gateway's
qBittorrent username/password. The UI holds those credentials in memory only;
use HTTPS outside a localhost port forward. WebDAV, SAB and qBittorrent retain
existing authentication regardless of the dashboard setting.

## Development

Requires Node.js 22.12+ and npm:

```sh
cd web
npm ci
npm run dev
```

Vite proxies `/dashboard/api` to `http://127.0.0.1:8080`. Forward the gateway there
first. In external mode your local port forward bypasses Authentik intentionally.
There is no sample-data fallback: failures are shown, and previous data is marked
stale. Requests poll every 10 seconds without overlapping.

## Build

From the repository root:

```sh
make release-build
```

This builds assets into `internal/webui/dist/`, then embeds them into the Go binary.
Compiled assets must be included in source commits so plain Go builds and source-release consumers
include a functioning dashboard. Rebuild them after frontend changes. The root
Dockerfile builds them in a Node stage; Node is not used at runtime.

The dashboard lists tenant-scoped jobs, browses catalog file metadata, and accepts
one magnet, torrent or NZB per submission (50 MiB file limit). Uploads use the same
retained-source and provider queue logic as Arr. Browsing does not read media or
generate provider links. Hidden download-client history remains browsable as
retained catalog files. Recovery scheduling is available through the diagnostics view; there are no delete or force-upload controls.

Provider progress is described below. Health reports successful catalog/database
access and pending verification, not active integration probes.

## Recovery & diagnostics

Search by an exact torrent infohash or gateway job ID, or select a retained job by
name. NZBs retain their separate job identity. The panel shows source kind and
validity, provider ID, attempts, missing confirmations, scheduled provider/controller
checks and up to 500 matching library rows. Original source contents, API keys and
raw provider errors are not returned.

**Check TorBox now** is read-only: it does not upload, modify job state, or increment
missing confirmations. Requests are limited to one per job every 15 seconds and
ten per gateway in that window. Unknown torrent submissions can be looked up by
exact hash, but this action does not adopt or resubmit them. Scheduled reconciliation
handles adoption. Unknown NZBs require provider inspection.

**Request rearm** requires a valid saved source, enabled recovery, a known provider
ID, three scheduled missing confirmations, and fewer than three attempts. Confirming
wakes the existing worker, which checks TorBox again before queuing a recovery.
The normal provider submission interval and attempt budget remain in force.
Failed/uncertain jobs cannot be reset from the dashboard. If source data is absent,
acquire a replacement through Sonarr/Radarr or submit its original source.

Download activity includes retained jobs marked hidden by Arr, labeled **Hidden
from client queue**. Hidden status is not deletion. Only downloads tracked by this
gateway appear; the dashboard does not invent history for pre-existing provider
placements. Current download-client visibility remains unchanged.

## TorBox progress and processing

Apply migration `034_provider_status.sql` before upgrading processes, or start the
schema-migrating gateway first, then roll out the controller. No credentials or new
configuration are needed. Provider telemetry is nullable for existing jobs until
their next scheduled provider check.

The gateway retains the raw TorBox state separately from Rehydrator's state.
For example, TorBox may report **Direct Unpack: Completed · Download 100%**, while
Rehydrator still waits for provider readiness. Download progress never authorizes
an import. Existing readiness and mounted-file verification rules still apply.

Optional fields include `progress` (fraction 0–1), `download_speed`, `size`,
`total_downloaded`, `download_finished`, `download_present`, and `cached`.
Unknown progress is not displayed as zero; invalid/negative metrics are discarded.
Only provider-supplied bytes and speed are displayed. No unpack percentage is
inferred, and no source URL/credential is retained in these observations.

Active provider jobs default to checks thirty seconds apart; completed jobs are
checked about fifteen minutes apart, subject to worker capacity and errors. The dashboard polls PostgreSQL every ten seconds, not TorBox.
It shows observation age and marks the last reading stale after a provider error.
TorBox itself may cache API responses, so these are observations, not a live stream.
A missing placement clears its old progress; replacement provider IDs discard
observations for the previous placement.

References: [TorBox API](https://api-docs.torbox.app/) and
[official Usenet SDK model](https://github.com/TorBox-App/torbox-sdk-py/blob/main/src/torbox_api/models/get_usenet_list_ok_response.py).

## Active polling interval

```yaml
gateway:
  active_poll_interval_seconds: 30
  submit_interval_seconds: 120
```

Environment override: `GATEWAY_ACTIVE_POLL_INTERVAL_SECONDS=30`. Values must be
15–3600 seconds. This schedules the first status check after submission and later
checks while either torrents or NZBs are downloading/processing. Existing jobs use
the new interval after their next due check; configuration requires a restart.

Provider checks while awaiting mount verification remain one minute apart;
completed placements stay at fifteen minutes and provider errors/missing placement
checks retain five-minute delays. Submission spacing remains independent. This does not change
Sonarr/Radarr's download-client polling, the controller's 15-second worker tick, or
the dashboard's 10-second database refresh. Due times are scheduling targets; the
worker's bounded capacity and provider response times can delay execution.

NZB staging uses a persisted release folder underneath each stable job directory
as of v0.8.1; apply migration 035 and upgrade both gateway/controller roles. Provider
progress and WebDAV paths are unaffected. See [SAB import paths](../docs/nzb-gateway-setup.md#release-named-import-folders-v081).

Denied OIDC group access shows a branded HTTP 403 page with a sign-in retry link.
It does not sign users out of Authentik; switch identity-provider accounts first
if you need to retry with a different identity.

## Appearance

The dashboard matches the shared Authentik theme's white / `#1e1e1e` page surfaces,
white / `#1a1a1a` cards, translucent inputs, one-rem corners and soft shadows.
Rehydrator keeps its green action accent with darker link text in light mode.
Choose System, Light or Dark in the header; the choice is stored per hostname as
`rehydrator-theme`. System tracks OS changes. No Authentik settings or credentials
are read, and preferences do not automatically sync between different hosts.

The script-free OIDC access-denied page uses the same colors and follows the OS
preference. It does not read the dashboard's local theme preference.

Terminal provider failure messages are sanitized. Failed NZBs are reported through
SAB history so Arr's configured failed-download handling can blocklist and search
for alternatives; see [failed NZB handling](../docs/nzb-gateway-setup.md#failed-downloads-and-replacement-searches-v084).
