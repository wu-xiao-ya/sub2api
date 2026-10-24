# Native Administrator Host Monitoring

The collector runs on the host. Do not mount the Docker socket into sub2api.
The administrator session protects these endpoints and the native page:

- GET /api/v1/admin/server-monitor/summary
- GET /api/v1/admin/server-monitor/logs?service=sub2api&lines=80
- /admin/server-monitor (also under the configured application base path)

## Configuration

Generate a random token of at least 32 characters. Store it in protected env files
(0600) for both processes, never in the browser, Git, URLs, or logs.

Collector environment:

    SERVER_MONITOR_TOKEN=<shared-random-secret>
    MONITOR_HOST=<private-host-bridge-address>
    MONITOR_PORT=18787
    MONITOR_SERVICE_NAME=server-monitor
    MONITOR_ACTIVE_RELEASE_FILE=/opt/server-monitor/active-release.json
    POSTGRES_CONTAINER=sub2api-hk-postgres
    REDIS_CONTAINER=sub2api-hk-redis
    EDGE_CONTAINER=starlight-relay-caddy-1

Application environment:

    SERVER_MONITOR_URL=http://<private-host-bridge-address>:18787
    SERVER_MONITOR_TOKEN=<same-shared-random-secret>

Invalid or missing application configuration fails closed with HTTP 503. The URL
must use HTTP/HTTPS and a private/loopback literal IP, localhost, or the explicitly
supported host.docker.internal name. No path, query, credentials, or redirect is
accepted. The collector defaults to loopback and requires a token.
For a host bridge binding, restrict inbound traffic to the application network.
Never publish port 18787 on the public interface or in the cloud firewall.

The active-release manifest names the routed container, not a candidate:

    {"container":"<active-container>","health_url":"http://127.0.0.1:<active-port>/health"}

Publish this JSON by atomic same-directory rename after successful edge cutover,
and restore the previous manifest during rollback. The collector reads it on each
sample/log request. Missing/invalid manifests report unhealthy and refuse app
logs; they never silently fall back to a stopped release.

## Authorized Deployment Checklist

1. Back up the collector, systemd/env configuration, edge routes and active release.
   Keep the existing application and monitor running.
2. Run the new collector on an unused private port first, with resource limits,
   the protected token, and a manifest referencing the current routed release.
3. Configure an isolated candidate app with that URL/token. Verify admin access,
   unauthenticated/non-admin rejection, wrong token, outage, redaction, refresh
   modes and ten-point tooltips.
4. Verify candidate root and /starlightai/ HTML, assets, API, streams, images,
   subscriptions, unchanged GPT-5.x pricing and Astra Fast pricing.
5. Only with deployment approval, switch the edge to the healthy candidate and
   atomically publish its manifest. Drain the prior application before stopping it.
6. Replace old /server-monitor with a redirect to the protected native page only
   after validation. Do not remove Basic Auth while retaining the old collector
   proxy. Test public collector port and old API denial.
7. Retain rollback image/configuration. Restore both edge and manifest on rollback.
   No database migration is required for this integration.

## Verification

Run python -m unittest test_server_monitor.py here. Frontend tests cover cadence,
off mode, visibility, cancellation and no overlaps. Backend tests cover URL
validation, credential isolation, query allowlists, redirects and bounded responses.

Page refreshes read cached samples without spawning Docker commands. The host
retains 240 samples in RAM and exports the latest 10. Logs are manual, limited to
20-200 lines and audited by sub2api. Logs are rendered as escaped text, never HTML.
