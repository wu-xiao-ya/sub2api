# Region Restriction

Sub2API can deny website and API access by country using a trusted CDN country
header. The feature is disabled by default and requires an application restart
after configuration changes.

## Cloudflare requirements

Cloudflare sends `CF-IPCountry` to proxied origins when IP geolocation is
enabled. The application must only trust this header when:

1. The DNS record is proxied through Cloudflare.
2. The origin firewall only accepts HTTP/HTTPS traffic from Cloudflare ranges.
3. Nginx overwrites or removes any client-supplied `CF-IPCountry` header before
   forwarding requests if another proxy is placed in front of Sub2API.

Do not enable this feature on a publicly reachable origin. A direct client can
otherwise omit or forge the country header.

## Configuration

```yaml
security:
  region_restriction:
    enabled: true
    country_header: "CF-IPCountry"
    blocked_countries:
      - "CN"
    effective_at: "2026-08-24T00:00:00+08:00"
    restricted_path: "/region-restricted"
    exempt_paths:
      - "/health"
      - "/api/v1/settings/public"
      - "/api/v1/setup/status"
      - "/api/v1/payment/webhook"
```

Environment-variable activation:

```text
SECURITY_REGION_RESTRICTION_ENABLED=true
SECURITY_REGION_RESTRICTION_EFFECTIVE_AT=2026-08-24T00:00:00+08:00
```

Keep `enabled=false` while validating a candidate image. Enabling it with the
example date takes effect at midnight on August 24, 2026 in Asia/Shanghai.

`CN` refers only to mainland China in Cloudflare's country code. Hong Kong
(`HK`), Macao (`MO`), Taiwan (`TW`), and all other countries or regions remain
available unless they are explicitly added to `blocked_countries`.

## Behavior

- Browser navigations receive a `307` redirect to `/region-restricted`.
- OpenAI-compatible endpoints receive an OpenAI permission error with code
  `REGION_RESTRICTED`.
- Anthropic and Gemini endpoints receive their native permission-error shapes.
- Console APIs receive the standard Sub2API `REGION_RESTRICTED` response.
- Missing country headers fail open to preserve health checks and local
  development. Origin firewalling is therefore mandatory.
- Health checks, public branding settings, setup status, payment webhooks, the
  restriction page, and its static assets remain accessible.

The restriction page intentionally contains no link back to the console.
