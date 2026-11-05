# Aggregate API keys

The restore branch preserves existing `api_keys.id`, key credentials and ordered
`api_key_aggregate_members` rows. Migration `256_restore_api_key_aggregate.sql`
creates this schema on fresh official installations and leaves existing bindings
intact. Previously applied migration files and checksums must not be rewritten.

## Behavior

- Users select 1–32 ordinary keys they own, from distinct groups, in priority order.
- HTTP requests select the first active, unexpired, non-exhausted member whose
  group catalog supports every possible parsed request model. JSON duplicate and
  case-variant model fields and multipart image edits use the native bounded parser.
- Root and member IP ACLs, current user/group grants, native subscription checks,
  model allowlists and monetary windows remain enforced. Group rate multipliers
  and native protocol dispatch use the selected member group.
- Usage records retain the root key ID. Wallet/subscription is charged once.
  Root and selected member counters commit in the same request-deduplicated SQL
  transaction. Aggregate billing fails closed if the atomic repository is absent.
- Auth caches hold the root kind, never member credentials. Memberships, member
  limits, group permissions and status are refreshed before aggregate routing.
- Catalogs union accessible member catalogs. Background image submissions,
  native Gemini endpoints and Responses WebSocket require a member key; the
  previous aggregate implementation did not provide safe support for these
  independent protocol/settlement surfaces. Standard HTTP/SSE and synchronous
  image requests use model selection.

## Verification and rollout

Use CI for generated-code consistency, unit/integration tests, frontend checks,
and a SHA-tagged candidate image. Tests cover ownership, nested/duplicate groups,
IP ACLs, model ambiguity, body preservation, membership transactions, duplicate
billing, member-counter failure rollback and ordered UI creation.

Before production update: exercise existing aggregate IDs on an isolated copy,
compare member/group routes and group pricing, verify one wallet deduction and
both key counters, then repeat a normal-key smoke test. Keep the current image
and a fresh database backup. This branch does not itself authorize a production
cutover and does not recreate or rotate any existing key.
