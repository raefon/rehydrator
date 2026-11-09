# Gateway frontend preview

Open `gateway.html` directly in a browser, or serve this directory:

```sh
python3 -m http.server 8090 --directory docs/mockups
```

Visit http://localhost:8090/gateway.html.

This standalone design preview uses sample data only. It includes protocol/search
filters, a simulated add-download dialog, folder navigation, and a health view.
No credentials, provider calls, uploads, deletions, or application routes are used.
Refreshing resets the sample queue. Files chosen in the dialog are never read.

The proposed interface separates TorBox processing, gateway publication, and
controller verification. A production version needs authenticated UI routes and
API contracts before these views can display real jobs or perform actions.

The React/TypeScript version now lives in [web](../../web/README.md). This HTML
file remains the original standalone visual reference.

## Unified live logs preview

Open [logs.html](logs.html) directly in a browser, or visit
http://localhost:8090/docs/mockups/logs.html when serving the repository root
with `python3 -m http.server 8090`.
The preview reuses `web/src/style.css`; serve the repository root so that
stylesheet remains accessible. Gateway and Core entries are simulated, with
source and level filters, text search, pause/resume, follow-newest, and themes.
Core labels represent the Rehydrator controller. The implemented dashboard now
uses shared PostgreSQL snapshots for split deployments; see [web/README.md](../../web/README.md)
for migration and retention details. This standalone preview stays simulated.
