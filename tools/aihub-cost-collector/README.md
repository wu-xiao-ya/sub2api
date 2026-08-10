# AIHub Cost Collector

This is a standalone, server-side collector for the rendered AIHub usage page.
It is intentionally separate from the Sub2API API, PostgreSQL, Redis, and
reverse proxy so an AIHub or browser failure cannot affect live requests.

## What it stores

The collector writes only:

- today's actual and standard cost;
- total request count;
- the rendered model, group, and endpoint summary tables;
- a timestamp and collector status.

It does not export cookies, passwords, API keys, usage-detail rows, or browser
profile data. Browser profile data is kept in a Docker named volume.

## Start

```bash
cp .env.example .env
docker compose up -d --build
docker compose logs -f collector
```

The Selenium and noVNC ports are bound to `127.0.0.1` on the server. Use an
SSH local port forward before opening noVNC:

```bash
ssh -L 17900:127.0.0.1:17900 root@YOUR_SERVER
```

Open `http://127.0.0.1:17900` locally, sign in to AIHub, and complete any
Cloudflare challenge. The browser profile then persists across restarts.

## Inspect snapshots

```bash
docker run --rm -v aihub-cost-collector_collector_data:/data alpine cat /data/latest.json
docker run --rm -v aihub-cost-collector_collector_data:/data alpine cat /data/status.json
```

## Limits

This is a temporary browser-based integration. Cloudflare or AIHub can require
an occasional manual re-login. A documented AIHub billing API should replace
this collector once one is available.
