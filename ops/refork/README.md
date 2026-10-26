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
  The full baseline build remains unproven until the corrected run completes.
- Migration runner unit tests (including the existing official checksum/non-tx
  suites) pass in a focused file-list invocation with Go 1.27.3.
- Nine Python unit tests pass. Database-copy integration tests have NOT run.
- Finance, image cost, manual rates, egress adapters, frontend verification,
  candidate deployment, billing regression, restore rehearsal and image digest
  are NOT delivered. Business integration waits for baseline verification.
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

1. Official migration 224 constrains quota platforms before migration 238 purges
   unlimited rows. Legacy platform rows can fail 224 first. A reviewed,
   archive-first sequencing solution and production-copy test are still needed.
   The tool does not silently delete rows, reorder migrations, or fake completion.
2. Production has 11 legacy-platform channel-pricing rows (GLM 5, Qwen 2, MiMo 2,
   Hunyuan 2). Their platform keys and channel mapping JSON must be converted
   without changing model sets, prices or multipliers. Group/account conversion
   alone is insufficient. The tool blocks these references pending that adapter.
3. Composite legacy target-platform references and limited legacy quota rows are
   blocked rather than naively merged. Historical monitoring/usage platforms must
   remain historical rather than being globally rewritten.
4. Migration 220 must not clear configured non-Grok video prices. Existing values
   block migration until an explicit preservation solution is accepted.
5. Migration 221 forces long-context pricing on; all data-changing pending SQL
   must be individually reviewed by exact filename and checksum. Any monetary
   differences block acceptance. Do not auto-approve the generated inventory.
6. New or unexpired legacy entitlements stop migration. Neither old subscription
   history nor its freeze migration is removed. Unfreeze is a forward operation
   after official migrations; the historical tables remain present.

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

Before any write: take a complete custom-format `pg_dump`, verify its digest,
restore it into independent candidate PostgreSQL, and prove no production
connection remains. The JSON archive is additional reconciliation evidence,
**not a complete database backup or a passed restore test**.

The reviewed conversion plan has this shape (illustrative model IDs only):

```json
{
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
  --plan ops/refork/private/plan.json --output ops/refork/private/archive.json
python ops/refork/refork.py migrate --candidate-database sub2api_refork_candidate \
  --archive ops/refork/private/archive.json --runner /private/bin/refork-migrate
python ops/refork/refork.py convert --candidate-database sub2api_refork_candidate \
  --archive ops/refork/private/archive.json
python ops/refork/refork.py audit --archive ops/refork/private/archive.json \
  --output ops/refork/private/audit.json
```

The archive checkpoint gates automatic startup migrations on restored custom
databases. While phase is archived, only the standalone runner with the matching
archive digest can proceed. Normal application startup stays blocked until
conversion. The runner uses the official advisory lock, transactions, checksum
checks and nontransactional index recovery. It does not execute SQL via a custom
splitter. Fresh official databases keep the upstream initialization path.

For an empty-database test, use only a new independent database with no
application objects, set `REFORK_INIT_EMPTY=true`,
`REFORK_CANDIDATE_DATABASE` and `DATABASE_URL`, then run `refork-migrate`.
The empty-init flag refuses any existing application table or view.

`audit` verifies old migration entries, identity sets, bindings, historical
usage digests, balances/orders/settings and archived quota removal. It is
fail-closed and may flag legitimate official column/default changes that need
explicit review. A passed data audit is never marked candidate acceptance.

## Acceptance Still Required

- Pure official baseline build, then final code generation, backend unit and DB
  integration tests; frontend typecheck, nonmutating lint, full Vitest and build.
- Dedicated PG/Redis/volumes/private ingress; no real payment/email/probes or
  other external side effects. Verify egress isolation, not just port binding.
- Full empty-init and restored-copy upgrade, rerun/idempotence and interrupted
  migration recovery. Prove an unarchived custom DB cannot start.
- Exact model admission/listing/account isolation after platform conversion.
- Fixed old/new billing samples: cache, long context, image costs, manual rate,
  currencies/rounding; no unexplained differences.
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
