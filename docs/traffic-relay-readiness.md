# Traffic Relay Readiness

Updated: 2026-09-19. Local implementation is not production-ready.

## Verified

- req/v3 shared clients now retain Chrome TLS impersonation while using bounded
  relay TCP/CONNECT hooks. Real stalled CONNECT tests pass for both ordinary
  and fingerprint clients.
- Antigravity privacy, quota and onboarding polling now route at each HTTP
  request boundary, preserving redirect restrictions and never restarting an
  earlier successful polling operation for a later CONNECT failure.
- Relay-only terminal errors carry explicit ownership. OpenAI transport error
  classification and token refresh cooldowns exclude them; failures on the
  fallback/default route retain their normal account error handling.
- September 19 verification: 200 frontend account/Toggle tests and frontend
  typecheck passed; broad targeted relay/OAuth/WebSocket/repository/audit tests
  passed locally.
- Tokyo loopback allow rules and localhost tinyproxy ACL entries were removed.
  Dedicated port-38480 firewall rules accept only 156.233.227.134; a systemd
  dependency installs them before tinyproxy on startup. Hong Kong CONNECT still
  returns 200 (unauthenticated origin 401); loopback and local non-HK access
  time out. nginx PIDs/80/443 listeners are unchanged; local website returns 302.
  Startup ordering was inspected, not tested by rebooting this shared server.
- An isolated Git candidate worktree now exists at
  D:\Documents\ssh服务器\tmp\sub2api-relay-candidate on
  codex/tokyo-relay-candidate-20260919. Original broken Git metadata was not
  overwritten and temporary credentials/scripts were excluded.
- OpenAI usage queries, reset-credit details and credit consumption now bind the
  resolved credential account's route and retain its own proxy as fallback.
  Reset fallback reuses the same redeem_request_id. Local tests passed for
  query fallback, subsequent cooldown, reset-ID preservation and existing
  QueryUsage/ResetCredit/prepare-operation paths.
- OpenAI privacy PATCH, account-info GET and subscription-expiry GET now use
  per-operation routing and fallback; background privacy checks bind the account
  route. req/v3 responses containing an actual HTTP response block replay, while
  empty wrappers on pre-connect failure remain retryable. Targeted privacy,
  account-operation, OAuth and refresh regressions passed locally.
- Ordinary gateway HTTP, the OpenAI/Grok shared WebSocket dialer, common HTTP
  clients and Antigravity clients now install an opt-in bounded CONNECT dialer.
  Only matching request-local relay markers activate it. Original proxy behavior
  remains in place for unmarked requests. Captured deadlines survive net/http's
  detached dial context; successful tunnels clear their connection deadline.
- Real local HTTP tests verify that a stalled relay falls back once to the
  account's own proxy, preserving POST body and authorization. WebSocket tests
  verify successful same-request direct fallback and skipping the relay during
  cooldown. Transport tests cover authenticated CONNECT, no proxy-credential
  leakage to the origin, streaming past the connect budget and tunnel reuse.
- Local unit-tagged proxyutil, tlsfingerprint, httpclient and Antigravity package
  tests passed. Targeted service, repository and administrator regressions for
  routing, OAuth, WebSocket, bulk updates and settings audits passed. CI has not
  yet executed the new race checks.
- Targeted local service, TLS-fingerprint and proxy utility tests pass after the
  CONNECT budget changes. Fingerprint relay TCP/CONNECT gets at most five seconds
  or half the remaining request deadline; origin TLS retains the parent context.
  Stalled CONNECT and origin TLS tests verify socket closure, fallback time and
  separation of origin failures from relay cooldown. Other proxy routes retain
  their original deadlines.
- Fingerprint transport tests classify actual CONNECT 403/407 as relay failures,
  exclude CONNECT 502/503/504, and reject errors belonging to a different proxy.
- Agent identity registration and account usage probes now carry operation-local
  routing; their targeted local regression tests passed.
- Hong Kong can establish an authenticated CONNECT tunnel through Tokyo.
  A credentials-free upstream models request returned HTTP 401 after CONNECT 200.
  This verifies connectivity, not authenticated model generation or latency gains.
- HTTP gateway, account test, and monitor calls using HTTPUpstream carry immutable
  request-local routes instead of a process-wide account-ID routing map.
- Request cancellation, non-replayable bodies, and response-plus-error results
  prevent unsafe HTTP replay. A non-replayable request still records relay outage.
- Actual HTTP CONNECT 407 integration test preserves POST body and authorization,
  and reaches the origin once through the fallback route.
- Concurrent same-account attempts retain their own default proxies.
- Redis and process-local outage keys are scoped to a hashed relay URL.
  In-flight successes cannot clear newer outage markers; markers expire by TTL.
- OpenAI pooled/passthrough WebSocket and Grok realtime dial paths have pre-handshake
  fallback. Opted-in pooled connections distinguish relay and fallback routes.
- Wire regeneration constructs the relay service as an explicit HTTP provider
  dependency; the base repository constructor retains its original concrete type.
- Frontend typecheck and 75 account/switch tests passed in the preceding work.
- Targeted service tests with the unit tag, OpenAI WebSocket pool tests, and
  repository HTTP upstream regression tests pass.
- Added single-network-operation fallback to OpenAI, Claude, Gemini, Grok and
  Antigravity refresh calls; refresh success is not replayed for enrichment errors.
  Cross-platform OpenAI/Claude/Gemini/Grok refresh tests preserve loaded own proxies.
- Vertex token exchange, OpenAI PAT validation, Gemini Drive/project/resource
  manager operations and Antigravity LoadCodeAssist use operation-level routing.
- Settings validate active HTTP proxy selection and cooldown range. Relay settings
  participate in audit diffs. Frontend proxy selection loads all active HTTP
  candidates; account modals refresh configuration on reopening.
- Bulk relay-only updates load and validate targets and preserve each parent's
  proxy when propagating to shadows. Dedicated mixed-proxy regression passes.
- Scheduler metadata preserves relay opt-in; full-cache roundtrip and legacy
  missing-field compatibility are tested.
- Redis tests cover cross-instance visibility, expiry, distinct relay configs,
  local fallback on Redis failure, and preventing late outages shortening TTL.
- Added a GitHub CI relay validation workflow and candidate-build prerequisite.
  Workflow includes isolated PostgreSQL migration repeat checks and race tests.
  This workflow has not been pushed or executed yet.
- Found a separate intact baseline checkout at commit
  311af471f069db53eebb30502c0dd039a8d89d28. Read-only comparison reports 106 changed
  tracked files and zero missing tracked files (new files require a separate audit).

## Release Blockers

- Finish the final cross-platform call-site audit and candidate integration
  validation. Unit tests are not proof of successful real model generation.
- Run race tests on CI and real Redis checks; local simulated-Redis tests passed.
- Review import/export and run the additive migration on an isolated database.
- Complete frontend UI browser interaction tests.
- Review the isolated candidate diff and new files before pushing; do not
  initialize/reset over the original broken worktree.
- Review full diff, push the verified source, build a GitHub CI candidate,
  validate the candidate database/image and frontend entry points.
- A broader local test selection also exposed failing Grok429WithoutQuotaHeaders
  and AdminResetQuota cases (the latter expect retired native-subscription
  behavior). Grok429WithoutQuotaHeaders and AdminResetQuota_ResetBoth both
  reproduce on untouched baseline 311af47; do not report the full service test
  suite as passing or restore retired subscription behavior just for these tests.

## Production Boundary

No production sub2api migration, restart, account opt-in, or traffic switch was
performed during this local verification. Production deployment still requires
separate authorization. Do not use the retired api.kiminonaha.top for acceptance.
