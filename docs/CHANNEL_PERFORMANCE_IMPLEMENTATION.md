# Channel Performance And Request Milestones

Status: implementation and validation in progress (2026-09-15). Not approved for production cutover.

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
- Persistent WebSocket turns now receive independent ingress clocks in pooled,
  passthrough and HTTP bridge paths. The first turn survives account retries and
  concurrency-context refresh; reused connections never invent response headers.
- Terminal WebSocket facts use a connection/turn identity independent of native
  billing response IDs. Migration 249 provides the optional native-ID join;
  no usage identity is rewritten. Failed retries remain pending, eventual success
  counts once, and failures without response IDs are retained. Passthrough facts
  are finalized after client delivery, independently of the earlier billing hook.
  Local concurrency limits are structurally excluded, while upstream saturation
  remains a failure even when both use the same WebSocket close code.
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
- Telemetry-only fact triggers invalidate both previous and new hours when a
  delayed witness changes start time. Retention deletes at most 2,000 rows per
  table/granularity per tick. The historical sweep repeats after initial backfill
  to recover lower usage IDs committed after the fast cursor passed them.
  This is eventual reconciliation, not immediate commit-order tracking: a full
  30-day sweep takes about six hours at one historical hour per 30-second tick,
  longer if database work is deferred. This freshness limit remains under review.
- Graceful shutdown persists the earliest observed telemetry gap after its final
  drain, including failures which occur after the last aggregation tick.
  Migration 250 adds independent runtime liveness: startup registration, 15-second
  heartbeats and clean-close markers after the final drain. A runtime stale for
  90 seconds conservatively marks coverage incomplete from its startup, covering
  lost long-running requests rather than guessing when loss began. The marker
  survives a replacement instance and never retires a healthy parallel candidate.
  This detects possible telemetry loss; it does not recover lost samples or
  provide exactly-once durable telemetry during a total database outage.
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
- Broad candidate frontend regression passed: 28 files, 179 tests, including the
  corrected CSV assertion for six new latency columns.
- Expanded service/handler/repository regression passed locally, including full
  WebSocket session, bridge and lifecycle tests. The September 15 rerun includes
  terminal attribution, concurrency ownership, monitor-probe exclusion and
  runtime liveness changes. PostgreSQL-specific tests skip locally; CI is required.
- 16 performance screenshots at 375/768/1440/1920px, light/dark: no page errors,
  no document overflow, all three charts painted. Shortened chart tick labels
  were rechecked. Preview uses synthetic data only.
- Real PostgreSQL integration tests are prepared: migration repeatability,
  free success vs paid interrupted stream, unknown legacy outcomes, probe
  exclusion, API-key-scoped IDs, cross-hour retry replacement, late usage,
  repeat recomputation, and absence of billing triggers.
- Local Docker engine is unavailable. Database tests have NOT yet executed locally.
  `.github/workflows/channel-performance-validation.yml` supplies isolated
  PostgreSQL 18 and must pass before accepting a candidate.
- Checkpoint 9a2cea0 candidate run 34860203493 passed the isolated PostgreSQL and
  focused frontend jobs. Its broad frontend CSV assertion failed and prevented
  image building; the assertion is now corrected. New database tests cover an
  earlier cross-hour witness and lower-ID late-commit reconciliation. The next
  CI must validate these additions; prior results do not cover them.
- The CI selection now includes full WebSocket session/bridge/lifecycle tests
  and a race-detection pass, not just latency helpers.
- Checkpoint 1a8a94468 passed CI, security scan, both frontend jobs, expanded
  backend tests, real PostgreSQL aggregation tests and the race detector. Its
  candidate image built successfully but the smoke script stopped before app
  startup: socket readiness matched PostgreSQL's temporary init server. The
  script now checks TCP readiness; candidate startup/publication must be rerun.
- Browser interaction checks passed for combined filters, off/5-second refresh,
  no detail requests after closing, and a visible chart tooltip with its actual
  timestamp/value. Synthetic test data only; these are not production probes.
- Checkpoint 67e775d6435f6a6f5c209ff8271855a3a1a6dbe6 passed candidate run
  34865601184, main CI 34865600923 and security 34865600927. Its candidate
  startup, root and /starlightai/ HTML, login and embedded assets passed.
  This evidence predates migrations 249/250 and must not validate the new delta.
- New real PostgreSQL tests cover native WebSocket usage joins across hours,
  unchanged billing identity, API-key isolation, runtime crashes, graceful close,
  stale-instance recovery and overlapping healthy instances. Migrations 248,
  249 and 250 execute twice in every isolated database fixture.

## Remaining Release Blockers

1. Validate the new WebSocket terminal attribution and runtime crash-coverage
   delta in CI, including the real PostgreSQL tests and concurrent race checks.
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
