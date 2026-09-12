# pi-review-dashboard

[English](README.md) | [中文](cn.md)

Cross-repository statistics dashboard for [pi-review-agent](https://github.com/sun-praise/pi-review-agent):
review counts, token usage (input / output / cacheRead / cacheWrite), cost, verdict distribution,
and cache hit rate — compared across repositories and tracked as daily trends.

Go backend + Vite/TypeScript frontend shipped as **a single static binary** that serves both the
API and the UI (frontend embedded via `go:embed`). State is **one SQLite file** — deploy by
copying the binary, back up by copying `data/stats.db`.

![dashboard preview (demo data, generated with `-seed 80`)](docs/screenshot.png)

```
repo A (CI, intranet runner) ─┐
repo B (CI)                   ─┼─ POST /api/events ─→ Go + SQLite ─→ Web UI
local CLI runs                ─┘    (idempotent dedupe)              (cross-repo compare/trends/details)
```

## Quick start

```bash
# 1. Build the frontend (produces web/dist, which then gets embedded into the binary)
cd web && npm ci && npm run build && cd ..

# 2. Build and run (requires Go 1.25+ — a modernc.org/sqlite v1.58 requirement;
#    with Go 1.21+ and GOTOOLCHAIN=auto the matching toolchain downloads automatically)
go build -o pi-review-dashboard .
./pi-review-dashboard                # http://localhost:8787

# Optional: seed 120 demo events first to see it in action (writes data/stats.db)
./pi-review-dashboard -seed 120
```

Dev mode: run `./pi-review-dashboard` in terminal 1 and `npm run dev` inside `web/` in
terminal 2 — Vite (:5173) proxies `/api` to the Go server (:8787).

## Configuration

| Env var | Default | Description |
|---|---|---|
| `PORT` | `8787` | Listen port |
| `STATS_DB` | `data/stats.db` | SQLite path |
| `STATS_TOKEN` | none | When set, ingest requires `Authorization: Bearer <token>`; can be left unset on a trusted intranet |

## Wiring up agents (per repository)

GitHub Actions, via action inputs:

```yaml
- uses: sun-praise/pi-review-agent@v1
  with:
    team: "quality:1,security:1"
    stats-url: http://<intranet-host>:8787/api/events
    stats-token: ${{ secrets.STATS_TOKEN }}   # only needed when the dashboard sets STATS_TOKEN
```

Local CLI:

```bash
PI_REVIEW_STATS_URL=http://<intranet-host>:8787/api/events npx tsx src/index.ts --pr 12 ...
```

Pushing is **fail-open**: a dashboard outage never affects the review itself; events are also
appended to `<sessions-root>/stats.jsonl` as a local fallback.

## Event protocol

Every completed review POSTs one JSON document (single object or array):

```json
{
  "schema": 1,
  "ts": "2026-09-08T12:34:56.000Z",
  "platform": "github",
  "repository": "owner/repo",
  "pr": 123, "runId": "987654321", "attempt": 1,
  "mode": "team",
  "personas": [
    { "name": "quality", "input": 81000, "output": 1200, "cacheRead": 64000,
      "cacheWrite": 0, "cost": 0.0112, "resumed": true }
  ],
  "verdict": "CAN MERGE",
  "severity": { "decision": "CAN MERGE", "blocking": 0, "warning": 1, "fallback": false },
  "usage": { "input": 250000, "output": 4000, "cacheRead": 190000, "cacheWrite": 0 },
  "costTotal": 0.038, "durationMs": 47000
}
```

- **Idempotent**: `(platform, repository, runId, attempt)` is unique — CI re-runs / HTTP retries count once;
- **Fields are additive-only** (versioned via `schema`), so third parties can parse them safely;
- Cache hit rate = `cacheRead / (input + cacheRead)`, measuring the money session resume actually saves.
- Event fields (repository, verdict, …) are untrusted input: the frontend routes every render
  through `esc()` output escaping (`web/src/format.ts`). When ingest runs without `STATS_TOKEN`
  this is the only line of defense — any new innerHTML interpolation point MUST go through it.

## Docker

```bash
docker build -t pi-review-dashboard .
docker run -d -p 8787:8787 -v /srv/pi-review:/data pi-review-dashboard
```

Or systemd:

```ini
[Unit]
Description=pi-review dashboard
After=network.target

[Service]
ExecStart=/opt/pi-review-dashboard/pi-review-dashboard
Environment=STATS_DB=/opt/pi-review-dashboard/data/stats.db
# Environment=STATS_TOKEN=change-me
WorkingDirectory=/opt/pi-review-dashboard
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Development

```bash
go test ./...     # store and API tests (idempotency / auth / aggregation)
cd web && npm run typecheck
```

Layout: `main.go` (entry / embed / SPA fallback) · `internal/store` (SQLite + aggregate
queries + seed) · `internal/api` (ingest + dashboard API) · `web/` (Vite + TS + Chart.js frontend).
