# Channel Performance And Request Milestones

Status: implementation and validation in progress (2026-09-14). Not approved for production cutover.

## Reference

Organization reference: QuantumNous/new-api, commit
`815217ba648ff800c4f583fbfa36ba286cee6b2d` (AGPL-3.0).
Reference paths: `web/src/features/pricing/components/model-details-performance.tsx`,
`model-details-charts.tsx`, `model-details-uptime-sparkline.tsx`,
`pkg/perf_metrics/metrics.go`. Independently implemented in existing Vue/Go;
no React source or runtime is included.

## Implemented

- Five versioned usage milestones, user/admin DTOs, CSV and shared usage table.
  V2 starts at logical HTTP ingress, retains its clock over retries, and freezes
  billing-worker snapshots. Legacy data keeps its original timing origin; missing
  measurements are never synthesized from first_token_ms.
- Transparent bounded HTTP/TLS body observer with Responses, Chat Completions,
  Anthropic, Gemini and Antigravity protocol parsing. HTTP-to-WebSocket event
  observations and Bedrock decoded-event observations are also connected.
- Model-card batch performance summary and permission-scoped detail endpoints.
  Reuse active channel/platform/model/group visibility before queries; cache keys
  include scopes and filters. Public DTOs contain no account, user or key details
  and do not expose global request counts.
- Independent terminal-fact queue, protocol completion witnesses, explicit unknown
  outcomes and sticky telemetry-loss coverage marker. Collection does not depend
  on Ops being enabled or its error log filters. Internal probes are excluded.
- Migration 248 adds independent facts, minute/hour buckets, dirty-hour journal
  and watermark. No billing-table trigger, historical billing mutation, extra
  usage log or active probe is added. A singleton background worker batches
  writes, bounds backfill and recomputation, and journals late usage by ID.
- Nullable weighted metrics, separate unknown dimensions, sparse trends, four
  ranges, model-exact grouping (not icon aliases), successful streaming-text TPS.
- Performance detail with group comparison, three Chart.js trends, point tooltips,
  range/group/tier/effort/stream filters and off/5/15/30/60-second refresh controls.
  Hidden pages and closed panels stop their queries; canceled/stale responses
  cannot overwrite current filters. Prices remain independent of errors.
- Preserved Opus/Sonnet/HY3 brand alias fixes and original monitor thresholds.

## Verification Evidence

- 81 focused frontend tests passed, including refresh suspension, stale-response
  handling, existing model plaza, latency table and brand registry.
- Frontend vue-tsc passed after performance panel integration.
- Focused backend service/handler/repository/routes/server tests passed before
  the final HTTP-to-WebSocket/Bedrock additions; rerun after those additions.
- 16 performance screenshots at 375/768/1440/1920px, light/dark: no page errors,
  no document overflow, all three charts painted. Final shortened chart tick
  labels need a refreshed screenshot pass. Preview uses synthetic data only.
- Real PostgreSQL integration tests are prepared: migration repeatability,
  free success vs paid interrupted stream, unknown legacy outcomes, probe
  exclusion, API-key-scoped IDs, cross-hour retry replacement, late usage,
  repeat recomputation, and absence of billing triggers.
- Local Docker engine is unavailable. Database tests have NOT yet executed locally.
  `.github/workflows/channel-performance-validation.yml` supplies isolated
  PostgreSQL 18 and must pass before accepting a candidate.

## Remaining Release Blockers

1. Persistent WebSocket sessions still need per-client-turn ingress clocks and
   terminal-fact attribution, including retries, HTTP bridge and passthrough
   modes. Never label handshake/connection age as a new turn's first response.
2. Complete credential/transport integration coverage (OAuth refresh, proxy/TLS,
   image, intermediate errors and disconnects) and validate all gateway variants.
3. Run real PostgreSQL tests and review query plans, retention/backfill bounds,
   transaction-out-of-order usage commits, late facts in both timestamp orders,
   and telemetry gap persistence across shutdown/restart.
4. Confirm coverage reflects the visible requested scope, not unrelated groups;
   verify per-metric low-sample indicators and all empty/error/unknown states.
5. Finish component interaction and keyboard/tooltip checks, final visual pass,
   broader business regression, GitHub candidate image and isolated full-entry
   validation. An image build alone is not authorization to bypass these blockers.

## Publication Boundary

The user conditionally authorizes seamless production cutover only when all
critical checks pass. Current production has not been modified by this feature.
Do not cut over while any blocker remains, and do not test the deprecated domain
api.kiminonaha.top. Retain candidate validation, hot entry switch, connection drain
and rollback. Do not combine the release with container cleanup or data correction.
