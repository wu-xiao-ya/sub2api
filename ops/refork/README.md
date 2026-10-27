# v0.2.18 Refork Candidate

Pinned upstream: `ac1b8f1271be0d674fa6f074403c1410f30a3a81`.
Branch: `codex/refork-v0.2.18-20261026`. Production cutover is not authorized.

## Current Status

This is a **draft implementation, not an accepted candidate**.

- A normal independent checkout and a draft PR exist; fork main is unchanged.
- Candidate CI builds the unmodified pinned baseline before testing modifications.
  It has no deployment steps and never writes production image tags.
- Actions resumed after the fork-wide disabled warning was observed. The first
  baseline run built the unmodified official frontend successfully, then failed
  on an unnecessary copy command in the candidate workflow (official Vite already
  writes directly into the backend embed directory). That command was removed.
  The corrected pinned official baseline build passed in run 40330396250 and
  uploaded artifact `official-v0.2.18-baseline` (artifact ID 12654457609).
  The official CI on commit 5cd65246e also passed all its jobs.
- Migration runner unit tests (including the existing official checksum/non-tx
  suites) pass in a focused file-list invocation with Go 1.27.3.
- Nineteen Python tests pass with a real local PostgreSQL 18.6 database:
  empty initialization, complete backup and independent restore, archive-before-
  cleanup, interrupted runner recovery, idempotent reruns, zero video-price
  preservation, platform/mapping/price conversion and fail-closed reconciliation.
  These use synthetic fixtures, NOT a successful production-copy upgrade.
- Finance ledger API/UI, image upstream cost settings/report adapters and manual
  upstream rate adapters are implemented but final integration validation is
  pending. Code generation exposed missing upstream tool dependencies; their
  exact checksums were added without changing application dependency versions.
- Egress adapters, restored-copy migration, full frontend verification, candidate
  deployment, complete billing regression, restore rehearsal and image digest
  are NOT delivered.
- Requested gpt-6-luna agents received unreadable encrypted payloads. They were
  stopped; no delegated output was accepted and no model was substituted.

## Migration Ledger

The read-only production export on 2026-10-26 contains 299 complete filenames.
Before the narrowly reviewed compatibility rules:

| State | Count |
| --- | ---: |
| Exact match | 238 |
| Custom-only historical filename | 56 |
| Official pending filename | 54 |
| Same-name checksum conflict | 5 |

Five deployed SQL files were verified against the old extraction snapshot using
the runner's actual algorithm: SHA256 over Go `strings.TrimSpace(content)`.
The pinned official SQL files are untouched. Compatibility is directional and
requires the exact filename, deployed checksum and pinned official checksum.
No migration record is rewritten, inserted speculatively or globally exempted.

| Filename | Reviewed Difference | Existing Data Treatment |
| --- | --- | --- |
| `195_channel_monitor_mode.sql` | Custom seed v2, official seed v1 | Do not rerun or overwrite the existing operator setting |
| `203_channel_monitor_v2_default_ignore_and_cache.sql` | Custom omits cache thresholds, official seeds 0.85/0.60 | Preserve the applied configuration |
| `204_channel_monitor_hide_throughput.sql` | Custom seed true, official false | Preserve the existing setting |
| `218_group_audio_voice_pricing.sql` | Comment-only SQL difference | Columns already exist; runtime NULL pricing semantics still need billing tests |
| `219_group_search_price_per_1k.sql` | Comment-only SQL difference | Column already exists; runtime NULL pricing semantics still need billing tests |

The complete private report is generated with `compare-ledger`. It must NOT be
committed. Schema hashes are not a substitute for schema or monetary validation.

## Blocking Data Compatibility Work

The coordinator intentionally refuses writes while any of these remain:

1. Before official 224, unlimited quotas are completely archived privately and in
   `refork_audit.quota_archive`. Only legacy-platform unlimited rows are removed
   ahead of the constraint; official 238 purges the remainder. Audit checks every
   archived counter. Limited legacy quota rows still block; they are not merged.
2. Production has 11 legacy-platform channel-pricing rows (GLM 5, Qwen 2, MiMo 2,
   Hunyuan 2). Their platform keys and channel mapping JSON must be converted
   without changing model sets, prices or multipliers. Group/account conversion
   alone is insufficient. The narrow adapter now converts these keys and blocks
   merged wildcard precedence and cross-platform model collisions.
3. Composite legacy target-platform references and limited legacy quota rows are
   blocked rather than naively merged. Historical monitoring/usage platforms must
   remain historical rather than being globally rewritten.
4. Migration 220 is unchanged. Conversion restores exact pre-migration video
   prices, including explicit zero, and records before/after restoration evidence.
   No app can start between cleanup and successful audit.
5. Migration 221 forces long-context pricing on; all data-changing pending SQL
   must be individually reviewed by exact filename and checksum. Any monetary
   differences block acceptance. Do not auto-approve the generated inventory.
6. New or unexpired legacy entitlements stop migration. Neither old subscription
   history nor its freeze migration is removed. Unfreeze is a forward operation
   after official migrations; the historical tables remain present.
7. The three production relay opt-ins still require a verified egress adapter.
   Preflight now BLOCKS them explicitly. Mapping them to proxy ID 1 would lose
   connect-failure fallback/cooldown semantics and is not an accepted substitute.

## Tool Operation

Use Python 3.12+, `requirements.txt`, PostgreSQL 18 and the compiled
`backend/cmd/refork-migrate` executable. Read `DATABASE_URL` from a private
environment, never a CLI DSN, workflow secret or committed file.

All write commands require the exact database name containing `candidate`.
They reject any other connected candidate client. Stop candidate applications
and background jobs for the entire procedure. Production must remain online and
untouched. `preflight` uses a read-only repeatable-read transaction.

Store reports under ignored `ops/refork/private/`, with restricted filesystem
permissions. Archives contain passwords, API keys and payment configuration.
They must never be uploaded as CI artifacts. Archive files are exclusively
created and content-digested; do not overwrite old evidence.

```sh
python -m pip install -r ops/refork/requirements.txt
python ops/refork/refork.py compare-ledger \
  --ledger ops/refork/private/live-ledger.json \
  --output ops/refork/private/ledger-reviewed.json
python ops/refork/refork.py preflight \
  --output ops/refork/private/preflight.json
```

Before migration: restore a complete production export into an independent
candidate. With the app stopped, create a custom-format backup of that candidate,
then restore it into a DIFFERENT empty independent candidate database using
`backup.py`. The tool fingerprints every public table and sequence, verifies the
dump digest, and issues a private proof. `archive` requires this proof and refuses
any intervening data drift. JSON evidence alone is never sufficient.

Both connections come from private `DATABASE_URL` environments. Database names
must contain `candidate`. Do not put DSNs/passwords in command arguments:

```sh
# DATABASE_URL selects the offline source candidate.
python ops/refork/backup.py create --candidate-database sub2api_refork_candidate \
  --dump ops/refork/private/full.dump --manifest ops/refork/private/backup.json
# DATABASE_URL now selects the DIFFERENT, empty restore candidate.
python ops/refork/backup.py verify --candidate-database sub2api_restore_candidate \
  --manifest ops/refork/private/backup.json --proof ops/refork/private/restore-proof.json
# DATABASE_URL must now select the original offline source candidate again.
```

The PostgreSQL client tools must match the server major version. Evidence paths
must stay accessible for verification. On Windows, restrict the private directory
ACL; POSIX file creation mode alone does not establish Windows access control.

The reviewed conversion plan has this shape (illustrative model IDs only):

```json
{
  "archive_unlimited_quotas_before_224": true,
  "restore_video_prices_after_220": true,
  "groups": {
    "33": {
      "from": "glm",
      "to": "zhipu",
      "account_ids": [101, 102],
      "model_allowlist": {"enabled": true, "models": ["operator-reviewed-model-id"]}
    }
  },
  "reviewed_cleanup": {
    "exact_filename.sql": "exact reviewed SHA256"
  }
}
```

Every converted group, including deleted historical groups, must have its actual
account IDs and exact client-visible model IDs reviewed. Wildcard/unbounded
discovery and fallback-group routing are refused. URLs, credentials, model
mapping and IDs are not guessed or replaced.

Only after all preflight blockers are resolved and the following flow has passed
database integration tests may an operator use the write commands:

```sh
python ops/refork/refork.py archive --candidate-database sub2api_refork_candidate \
  --plan ops/refork/private/plan.json --output ops/refork/private/archive.json \
  --backup-proof ops/refork/private/restore-proof.json
python ops/refork/refork.py migrate --candidate-database sub2api_refork_candidate \
  --archive ops/refork/private/archive.json --runner /private/bin/refork-migrate
python ops/refork/refork.py convert --candidate-database sub2api_refork_candidate \
  --archive ops/refork/private/archive.json
python ops/refork/refork.py audit --candidate-database sub2api_refork_candidate \
  --archive ops/refork/private/archive.json \
  --output ops/refork/private/audit.json
```

The archive checkpoint gates automatic startup migrations on restored custom
databases. While the official migration phase is incomplete, only the standalone
runner with the matching archive digest can proceed. Normal application startup
stays blocked until successful AUDIT, not merely conversion. The runner uses the
official advisory lock, transactions, checksum
checks and nontransactional index recovery. It does not execute SQL via a custom
splitter. Fresh official databases keep the upstream initialization path.

For an empty-database test, use only a new independent database with no
application objects, set `REFORK_INIT_EMPTY=true`,
`REFORK_CANDIDATE_DATABASE` and `DATABASE_URL`, then run `refork-migrate`.
The empty-init flag refuses any existing application table or view.

`audit` verifies old migration entries, identity sets, bindings, historical
usage digests, balances/orders/settings, every other historical public table and
archived quota removal. The only accepted account-extra addition is the official
175 trigger's exact long-context boolean default on conversion to OpenAI; existing
extra keys stay unchanged. Official cache-invalidation events may append only
known API-key hashes; existing outbox rows cannot change or disappear. It is
fail-closed and may flag legitimate official column/default changes that need
explicit review. A passed data audit is never marked candidate acceptance.

## Isolated Linux Smoke Candidate

`candidate-compose.yaml` runs the digest-pinned application with its own
PostgreSQL, Redis and persistent volumes. The Docker network is internal-only;
the app uses `172.31.240.2:8080`, publishes no host port, and the host firewall
denies new connections from the candidate subnet to host services. Keep its
generated `.env` root-readable only and out of Git.

For browser access, create an SSH tunnel to the candidate host:

```sh
ssh -N -L 19098:172.31.240.2:8080 <candidate-host>
```

Then open `http://127.0.0.1:19098`. This environment currently proves clean
database initialization and basic HTTP/UI health only. It contains no restored
production data or provider credentials and has no outbound route, so it does
not prove migrated behavior or live inference. Do not treat it as an accepted
candidate or production authorization.

## Acceptance Still Required

- Pure official baseline build, then final code generation, backend unit and DB
  integration tests; frontend typecheck, nonmutating lint, full Vitest and build.
- Production-copy upgrade and restore rehearsal in a Linux environment using
  the production collation. The smoke candidate above contains an empty DB.
- Verify side-effect workers remain disabled before enabling any candidate
  egress. Prove the one currently available user-facing relay path; alternate
  relay failover is not required while no alternate route exists.
- Full empty-init and restored-copy upgrade, rerun/idempotence and interrupted
  migration recovery. Prove an unarchived custom DB cannot start.
- Exact model admission/listing/account isolation after platform conversion.
- Fixed old/new user-billing samples: cache, long context, manual rate and
  currency rounding; no unexplained differences.
- Image upstream-cost/profit reporting consistency is deferred because it does
  not affect user-facing inference or charges.
- Native subscription purchase/deduction and payment idempotence with mocks;
  multi-protocol SSE success/failure and Gemini cache ownership.
- Same-session Pro Max cache replay using genuine parsed usage, no fixed
  whole-group hit-rate target and no statistical rewriting.
- PR review, immutable image digest, private restore report and production
  cutover/rollback manual. Candidate approval is NOT production authorization.

## Recovery Boundary

No migration tool writes to production. If candidate migration fails, stop the
candidate, preserve its logs/archive/checkpoints, create a NEW candidate database
from the verified dump, and compare its original ledger/identity/monetary totals.
Do not roll back SQL in place, alter applied migration records, or reuse the
modified candidate database as the backup. A production cutover manual remains
pending; this document does not authorize deployment or destruction of volumes.
