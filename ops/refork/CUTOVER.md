# Future Cutover and Recovery Runbook

This is an operator checklist, NOT authorization to change production. The current
delivery must remain a private candidate. Do not execute this runbook until every
acceptance item in README.md has evidence and the owner separately approves a
production window.

## 1. Establish the Release Identity

- Resolve the branch commit, upstream ancestry and image `@sha256` digest again.
  Never use `latest`, a production tag or an unverified locally rebuilt binary.
- Record the exact application, ingress, PostgreSQL, Redis, data volume and
  encryption-key references. Read live names; do not rely on an earlier report.
- Preserve all existing branches, the current main ref, the running image digest,
  application configuration and ingress configuration in private rollback storage.
- Verify that the candidate has its own database, Redis, volumes and private
  ingress. An application connected to production data or cache is not a candidate.
- Verify database roles, object ownership and grants after restore. The backup
  intentionally uses no-owner/no-privileges; it is not a replacement for a
  least-privilege application-role check.

## 2. Complete Candidate Acceptance

Every result must identify the code commit, image digest, dump digest, reviewed
conversion plan, archive digest and actual database under test.

- Pass empty initialization, production-copy upgrade, interrupted migration
  recovery, idempotent reruns and restoration from a complete dump.
- Review every pending data-changing migration by complete filename and its
  actual runner checksum. Never edit an applied migration or its ledger row.
- Verify original IDs and all user, account, key and group bindings, every old
  balance, payment/order history, usage digest and financial ledger total.
- Verify explicit exact model scope and no fallback to another group/account.
  GLM becomes zhipu; Qwen/MiMo/Hunyuan remain distinct OpenAI-compatible groups.
- Prove relay behavior for every active opt-in: selected relay, account-default
  fallback, connect-failure cooldown, cancellation, replay safety and applicable
  HTTP/WebSocket paths. Static proxy reassignment is not an equivalent fallback.
- Reconcile cached input, long context, image costs, manual rates, currency and
  rounding against fixed old/new samples. Check raw and aggregated reports after
  changing image-cost settings. Explain every intended difference.
- Exercise protocol SSE success/failure, Gemini cache ownership, payment
  idempotency and native subscription purchase/deduction using mocks.
- Replay the same Pro Max session workload against both candidates and compare
  genuine parsed usage. No fixed whole-group cache target or synthetic adjustment.
- Prove real payment, mail, OAuth refresh, scheduled probes, cleanup and other
  external jobs cannot run. A loopback port alone does not establish egress
  isolation; verify deny-by-default network policy as well.

## 3. Prepare a Fresh Final Snapshot

An earlier rehearsal dump is not the final cutover snapshot.

1. Announce the approved maintenance window and stop new balance/order/account
   writes. Drain in-flight inference, billing queues and asynchronous callbacks.
2. Stop all old workers that can mutate the database. Capture pending callback
   and payment reconciliation state privately; ensure providers can retry safely.
3. Recheck active legacy entitlements immediately. Any new entitlement stops the
   procedure. Do not expire, delete or silently convert it.
4. Export a complete consistent custom-format dump using read-only source access.
   Retain a digest, the source identity and private transport evidence. Do not
   upload the dump, credentials or reconciliation evidence to GitHub.
5. Restore into a new independent candidate. Run `backup.py create` and
   `backup.py verify` against two separate candidate databases; retain the full
   per-table and sequence proof.
6. Stop candidate clients. Run preflight, reviewed archive, official migrations,
   conversion and audit in that order. If any comparison fails, keep maintenance
   and investigate or abandon the attempt. Never bypass the checks.
7. Repeat final configuration/billing/identity reconciliation against this exact
   snapshot. Preserve original video prices, zero prices and every unlimited
   quota counter before cleanup.

The coordinator is deliberately candidate-only. It is not a generic in-place
production migrator. Promotion of a verified database, its final logical name,
ownership and connection configuration need a separate reviewed operation.

## 4. Promote Only After Separate Approval

- Keep the original production database and volumes intact. Do not replace or
  reset them with the official source tree or an empty database.
- Stop both applications before changing bindings. Carry required encryption
  keys through private configuration, not repository files. Do not accidentally
  rotate TOTP/payment encryption keys during an image replacement.
- Bind the verified image, independent upgraded database and independent Redis.
  Confirm the final database identity, schema ledger and image digest before
  exposing ingress.
- Enable private smoke traffic first. Check health, logs, auth/key binding,
  model admission, routing and small controlled billing samples.
- Establish a single authoritative callback and billing writer. Never let the
  old and new workers fulfill the same payment against different databases.
- Reopen writes/traffic only after recording the exact promotion time and last
  reconciled order/usage boundaries. Re-enable external jobs individually after
  validating their recipients, endpoints, schedules and idempotency.
- Observe actual errors, streaming completion and account/group isolation.
  A health endpoint alone is not sufficient acceptance.

## 5. Recovery Boundaries

### Before New Financial Writes

Stop the candidate and its workers, preserve logs/checkpoints, and return ingress
and application configuration to the verified old service/database/cache tuple.
Confirm that no candidate order, balance or usage writes need reconciliation.
The untouched original service is the rollback source, not a partially upgraded
database.

For a failed rehearsal, restore the immutable full dump into a NEW empty
candidate database. Never reverse applied SQL, alter migration checksums, mark
unexecuted SQL complete or reuse a modified candidate as the backup.

### After New Financial Writes

Do NOT switch blindly to the old database or restore the pre-cutover dump. Either
would lose post-cutover balances, orders or usage. Freeze new writes, capture the
new database and callback state, reconcile every post-cutover financial movement,
and choose a reviewed forward repair or an explicitly reconciled rollback.

Retain old and new images, both databases, full dumps, archives and private
evidence until the owner approves retirement. Never remove historical subscription,
contribution, promotion or quota archives as part of cleanup.
