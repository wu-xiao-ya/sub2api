#!/usr/bin/env python3
"""Fail-closed candidate migration coordinator; DATABASE_URL is never logged."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal
from functools import partial

BASELINE = "ac1b8f1271be0d674fa6f074403c1410f30a3a81"
ROOT = Path(__file__).resolve().parents[2]
MIGRATIONS = ROOT / "backend" / "migrations"
LOCK_ID = 694208311321144027
PLATFORMS = {"glm": "zhipu", "qwen": "openai", "mimo": "openai", "hunyuan": "openai"}
# Go strings.TrimSpace uses Unicode White_Space, not Python's extra C0 separators.
GO_SPACE = " \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"


class Blocked(Exception):
    pass


def checksum(content):
    if isinstance(content, bytes):
        content = content.decode("utf-8")
    return hashlib.sha256(content.strip(GO_SPACE).encode("utf-8")).hexdigest()


def official_ledger():
    return {p.name: checksum(p.read_bytes()) for p in sorted(MIGRATIONS.glob("*.sql"))
            if p.read_text(encoding="utf-8").strip(GO_SPACE)}


def compatibility_rules():
    """Read exact official runner rules; no duplicated or broadened allowlist."""
    source = (ROOT / "backend/internal/repository/migrations_runner.go").read_text(encoding="utf-8")
    rules = {}
    pattern = r'"([^"]+\.sql)"\s*:\s*newMigrationChecksumCompatibilityRule\(([^)]*)\)'
    for name, values in re.findall(pattern, source):
        rules[name] = set(re.findall(r'"([0-9a-f]{64})"', values))
    return rules


def exact_compatibility_rules():
    source = (ROOT / "backend/internal/repository/refork_migration_checksums.go").read_text(encoding="utf-8")
    pattern = r'"([^"]+\.sql)"\s*:\s*newReforkMigrationChecksumCompatibilityRule\(\s*"([0-9a-f]{64})",\s*"([0-9a-f]{64})"\)'
    return {name: (db, file) for name, file, db in re.findall(pattern, source)}


def compare_ledgers(applied, files=None, rules=None, exact_rules=None):
    files = official_ledger() if files is None else files
    rules = compatibility_rules() if rules is None else rules
    exact_rules = exact_compatibility_rules() if exact_rules is None else exact_rules
    rows = []
    for name in sorted(set(applied) | set(files)):
        old, new = applied.get(name), files.get(name)
        state = "pending" if old is None else "legacy_only" if new is None else "match"
        if old is not None and new is not None and old != new:
            known = rules.get(name, set())
            approved = (old in known and new in known) or exact_rules.get(name) == (old, new)
            state = "compatible" if approved else "blocked_checksum"
        rows.append({"filename": name, "database_checksum": old,
                     "file_checksum": new, "status": state})
    return rows


def exact_models(config):
    if not isinstance(config, dict) or config.get("enabled") is not True:
        raise Blocked("converted groups require an explicit enabled model_allowlist")
    models = config.get("models")
    if not isinstance(models, list) or not models:
        raise Blocked("converted group model_allowlist must not be empty")
    result = []
    for model in models:
        if not isinstance(model, str) or not model.strip() or "*" in model:
            raise Blocked("conversion requires exact model IDs, not wildcard discovery")
        if model.strip().lower() not in {m.lower() for m in result}:
            result.append(model.strip())
    return {"enabled": True, "models": result}


def safe_json(value):
    return json.dumps(value, default=str, sort_keys=True, separators=(",", ":"), ensure_ascii=True)


def archive_digest(archive):
    payload = {key: value for key, value in archive.items() if key != "sha256"}
    return hashlib.sha256(safe_json(payload).encode("utf-8")).hexdigest()


def write_private(path, data):
    path = Path(path).resolve()
    private = (Path(__file__).parent / "private").resolve()
    if ROOT in path.parents and private not in path.parents:
        raise Blocked("repository evidence must be written under ignored ops/refork/private/")
    path.parent.mkdir(parents=True, exist_ok=True)
    # Exclusive create prevents overwriting the evidence used to approve a run.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
        stream.write(json.dumps(data, default=str, indent=2, sort_keys=True) + "\n")


def table_exists(conn, name):
    return conn.execute("SELECT to_regclass(%s) IS NOT NULL", (f"public.{name}",)).fetchone()[0]


def snapshot(conn, usage_columns=None):
    """Capture configuration/identity evidence locally; never upload this to CI."""
    tables = (
        "users", "api_keys", "accounts", "groups", "account_groups",
        "user_allowed_groups", "user_group_rates", "user_platform_quotas",
        "user_subscriptions", "subscription_purchases", "subscription_purchase_groups",
        "subscription_plans", "subscription_plan_groups", "group_promotions",
        "account_contribution_logs", "payment_orders", "payment_audit_logs",
        "user_affiliate_ledger", "redeem_codes", "channels", "channel_groups",
        "channel_model_pricing", "channel_account_stats_model_pricing", "composite_model_routes",
    )
    evidence = {}
    for name in tables:
        if table_exists(conn, name):
            # Some join tables have composite primary keys and no id.
            data = [json.loads(safe_json(row[0])) for row in conn.execute(f"SELECT to_jsonb(t) FROM public.{name} t")]
            evidence[name] = sorted(data, key=safe_json)
    evidence["schema_migrations"] = [dict(zip(("filename", "checksum"), row)) for row in conn.execute(
        "SELECT filename, checksum FROM schema_migrations ORDER BY filename")]
    # Settings can contain provider credentials. They stay inside the private archive.
    if table_exists(conn, "settings"):
        evidence["settings"] = [json.loads(safe_json(row[0])) for row in conn.execute(
            "SELECT to_jsonb(t) FROM settings t ORDER BY key")]
    evidence["usage_logs"] = usage_fingerprint(conn, usage_columns)
    return evidence


def projected_rows_match(before, after):
    """Official migrations may add columns, but cannot rewrite original fields."""
    if not before:
        return not after
    columns = set(before[0])
    if any(set(row) != columns for row in before):
        raise Blocked("inconsistent archived row columns")
    projected = [{key: row.get(key) for key in columns} for row in after]
    return sorted(before, key=safe_json) == sorted(projected, key=safe_json)


def usage_fingerprint(conn, columns=None):
    if not table_exists(conn, "usage_logs"):
        return None
    # Full row digest catches monetary or historical rewrites, not just row counts.
    digest = hashlib.sha256()
    count = 0
    if columns is None:
        columns = [row[0] for row in conn.execute("""
            SELECT column_name FROM information_schema.columns
            WHERE table_schema='public' AND table_name='usage_logs' ORDER BY ordinal_position
        """)]
    with conn.cursor(name="refork_usage_audit") as cursor:
        cursor.execute("SELECT to_jsonb(t) FROM usage_logs t ORDER BY id")
        for (row,) in cursor:
            row = {key: row[key] for key in columns}
            digest.update(safe_json(row).encode("utf-8") + b"\n")
            count += 1
    return {"count": count, "sha256": digest.hexdigest(), "columns": columns}


def ledger(conn):
    if not table_exists(conn, "schema_migrations"):
        return {}
    return dict(conn.execute("SELECT filename, checksum FROM schema_migrations ORDER BY filename"))


def checks(conn):
    problems = []
    comparison = compare_ledgers(ledger(conn))
    problems += [f"checksum mismatch: {r['filename']}" for r in comparison
                 if r["status"] == "blocked_checksum"]
    if table_exists(conn, "subscription_purchases"):
        count = conn.execute("""
            SELECT count(*) FROM subscription_purchases
            WHERE lower(status) = 'active' AND expires_at > now()
        """).fetchone()[0]
        if count:
            problems.append("active legacy subscription purchases exist")
    if table_exists(conn, "user_subscriptions"):
        count = conn.execute("""
            SELECT count(*) FROM user_subscriptions
            WHERE deleted_at IS NULL AND lower(status) = 'active' AND expires_at > now()
        """).fetchone()[0]
        if count:
            problems.append("active native subscription entitlements require review")
    if table_exists(conn, "user_platform_quotas"):
        count = conn.execute("""
            SELECT count(*) FROM user_platform_quotas
            WHERE platform IN ('glm','qwen','mimo','hunyuan')
              AND (daily_limit_usd IS NOT NULL OR weekly_limit_usd IS NOT NULL OR monthly_limit_usd IS NOT NULL)
        """).fetchone()[0]
        if count:
            problems.append("limited legacy platform quota conversion requires explicit reconciliation")
    pending = {r["filename"] for r in comparison if r["status"] == "pending"}
    if "224_user_platform_quotas_add_cn_providers.sql" in pending and table_exists(conn, "user_platform_quotas"):
        count = conn.execute("""
            SELECT count(*) FROM user_platform_quotas
            WHERE platform NOT IN ('anthropic','openai','gemini','antigravity','grok','kimi','zhipu','deepseek')
        """).fetchone()[0]
        if count:
            problems.append("official migration 224 would reject legacy quota platforms before cleanup 238; sequencing review required")
    for table, column in (("composite_model_routes", "target_platform"),
                          ("channel_model_pricing", "platform"),
                          ("channel_account_stats_model_pricing", "platform")):
        if table_exists(conn, table):
            count = conn.execute(f"SELECT count(*) FROM {table} WHERE {column}=ANY(%s)",
                                 (list(PLATFORMS),)).fetchone()[0]
            if count:
                problems.append(f"legacy platform references in {table} require reviewed routing/pricing conversion")
    cleanup = []
    for name in sorted(pending):
        text = (MIGRATIONS / name).read_text(encoding="utf-8")
        # This is a review inventory, not an SQL parser or automatic approval.
        if re.search(r"\b(DELETE\s+FROM|DROP|TRUNCATE|UPDATE)\b", text, re.I):
            cleanup.append({"filename": name, "checksum": checksum(text)})
    if "220_clear_non_grok_video_generation_config.sql" in pending and table_exists(conn, "groups"):
        count = conn.execute("""
            SELECT count(*) FROM groups WHERE platform IS DISTINCT FROM 'grok'
              AND platform IS DISTINCT FROM 'composite'
              AND (video_price_480p IS NOT NULL OR video_price_720p IS NOT NULL
                   OR video_price_1080p IS NOT NULL OR video_model_prices IS NOT NULL)
        """).fetchone()[0]
        if count:
            problems.append("migration 220 would clear existing video prices")
    return {"baseline": BASELINE, "applied_count": len(ledger(conn)),
            "ledger": comparison, "cleanup_review": cleanup, "blockers": problems}


def connect(read_only=False):
    import psycopg
    from psycopg.types.json import set_json_loads
    dsn = os.environ.get("DATABASE_URL")
    if not dsn:
        raise Blocked("DATABASE_URL is required")
    conn = psycopg.connect(dsn)
    # PostgreSQL NUMERIC values must never be rounded through binary float.
    set_json_loads(partial(json.loads, parse_float=Decimal), conn)
    conn.execute("SET statement_timeout = '120s'")
    if read_only:
        conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY")
    return conn


def candidate_guard(conn, expected):
    identity = conn.execute("SELECT current_database()").fetchone()[0]
    if not expected or identity != expected or "candidate" not in identity.lower():
        raise Blocked("write operations require the exact independent candidate database name")
    conn.execute("SELECT pg_advisory_xact_lock(%s)", (LOCK_ID,))
    others = conn.execute("""
        SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
          AND pid<>pg_backend_pid() AND backend_type='client backend'
    """).fetchone()[0]
    if others:
        raise Blocked("stop candidate applications and other candidate database clients first")


def load_archive(path):
    archive = json.loads(Path(path).read_text(encoding="utf-8"))
    if archive.get("baseline") != BASELINE:
        raise Blocked("archive baseline mismatch")
    expected = archive_digest(archive)
    if archive.get("sha256") != expected:
        raise Blocked("archive digest mismatch")
    return archive


def checkpoint(conn, archive, expected_database, phases):
    if archive.get("database") != expected_database:
        raise Blocked("archive database mismatch")
    result = conn.execute("""
        SELECT archive_sha256, phase FROM refork_audit.checkpoint WHERE baseline=%s FOR UPDATE
    """, (BASELINE,)).fetchone()
    if not result or result[0] != archive["sha256"] or result[1] not in phases:
        raise Blocked("archive checkpoint mismatch or invalid operation phase")
    return result[1]


def original_ledger_intact(conn, archive):
    current = ledger(conn)
    original = {row["filename"]: row["checksum"] for row in archive["data"]["schema_migrations"]}
    if any(current.get(name) != value for name, value in original.items()):
        raise Blocked("original applied migration records changed")
    known = official_ledger()
    if any(name not in original and (name not in known or known[name] != value)
           for name, value in current.items()):
        raise Blocked("unexpected migration records appeared after archive")


def validate_plan(conn, plan):
    changes = []
    groups = plan.get("groups", {})
    for group_id, old in conn.execute("SELECT id, platform FROM groups WHERE platform = ANY(%s) ORDER BY id",
                                     (list(PLATFORMS),)):
        item = groups.get(str(group_id))
        if not item or item.get("from") != old or item.get("to") != PLATFORMS[old]:
            raise Blocked(f"group {group_id} is missing its reviewed platform mapping")
        allowlist = exact_models(item.get("model_allowlist"))
        actual = [r[0] for r in conn.execute(
            "SELECT account_id FROM account_groups WHERE group_id=%s ORDER BY account_id", (group_id,))]
        if item.get("account_ids") != actual:
            raise Blocked(f"group {group_id} account bindings must be reviewed explicitly")
        fallback = conn.execute("""
            SELECT fallback_group_id, fallback_group_id_on_invalid_request FROM groups WHERE id=%s
        """, (group_id,)).fetchone()
        if any(value is not None for value in fallback):
            raise Blocked(f"group {group_id} fallback routing requires a separate compatibility review")
        changes.append((group_id, old, PLATFORMS[old], allowlist))
    # No guessed mapping keys, host URLs, protocols, or model catalogs.
    return changes


def ensure_no_blockers(report):
    if report["blockers"]:
        raise Blocked("; ".join(report["blockers"]))


def do_compare_ledger(args):
    applied = json.loads(Path(args.ledger).read_text(encoding="utf-8-sig"))
    if not isinstance(applied, dict) or any(
        not isinstance(name, str) or not isinstance(value, str)
        or not re.fullmatch(r"[0-9a-f]{64}", value)
        for name, value in applied.items()
    ):
        raise Blocked("ledger must map complete filenames to SHA256 checksums")
    comparison = compare_ledgers(applied)
    counts = dict(Counter(row["status"] for row in comparison))
    write_private(args.output, {"baseline": BASELINE, "applied_count": len(applied),
                                "counts": counts, "ledger": comparison})
    print(safe_json(counts))
    if counts.get("blocked_checksum", 0):
        raise Blocked("unreviewed checksum differences remain")


def do_preflight(args):
    with connect(read_only=True) as conn:
        report = checks(conn)
    write_private(args.output, report)
    print(f"preflight: applied={report['applied_count']} blockers={len(report['blockers'])}")
    ensure_no_blockers(report)


def do_archive(args):
    with connect() as conn:
        conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
        candidate_guard(conn, args.candidate_database)
        conn.execute("LOCK TABLE groups, accounts, user_subscriptions IN SHARE ROW EXCLUSIVE MODE")
        report = checks(conn)
        ensure_no_blockers(report)
        plan = json.loads(Path(args.plan).read_text(encoding="utf-8"))
        validate_plan(conn, plan)
        approved = plan.get("reviewed_cleanup", {})
        for item in report["cleanup_review"]:
            if approved.get(item["filename"]) != item["checksum"]:
                raise Blocked(f"pending data-changing migration needs review: {item['filename']}")
        data = snapshot(conn)
        archive = {"baseline": BASELINE, "database": args.candidate_database,
                   "created_at": datetime.now(timezone.utc).isoformat(), "report": report,
                   "plan": plan, "data": data}
        archive["sha256"] = archive_digest(archive)
        write_private(args.output, archive)
        conn.execute("""
            CREATE SCHEMA IF NOT EXISTS refork_audit;
            CREATE TABLE IF NOT EXISTS refork_audit.checkpoint (
              baseline text PRIMARY KEY, archive_sha256 text NOT NULL,
              phase text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
            )
        """)
        conn.execute("""
            INSERT INTO refork_audit.checkpoint (baseline,archive_sha256,phase)
            VALUES (%s,%s,'archived') ON CONFLICT (baseline)
            DO UPDATE SET archive_sha256=EXCLUDED.archive_sha256, phase='archived', created_at=now()
        """, (BASELINE, archive["sha256"]))
    print("archive complete; private evidence must be retained outside GitHub")


def do_migrate(args):
    archive = load_archive(args.archive)
    with connect() as conn:
        candidate_guard(conn, args.candidate_database)
        phase = checkpoint(conn, archive, args.candidate_database, ("archived", "migrating", "migrated"))
        ensure_no_blockers(checks(conn))
        original_ledger_intact(conn, archive)
        if phase == "archived":
            usage_columns = (archive["data"].get("usage_logs") or {}).get("columns")
            if snapshot(conn, usage_columns) != archive["data"]:
                raise Blocked("candidate data drift since archive; re-archive before migrations")
        conn.execute("UPDATE refork_audit.checkpoint SET phase='migrating' WHERE baseline=%s", (BASELINE,))
    # The executable uses the official runner, checksum rules and non-tx handling.
    # Its own preflight guard checks the ledger again. No fabricated migration rows.
    env = dict(os.environ)
    env["REFORK_ARCHIVE_SHA256"] = archive["sha256"]
    env["REFORK_CANDIDATE_DATABASE"] = args.candidate_database
    subprocess.run([str(Path(args.runner).resolve())], env=env, check=True)
    with connect() as conn:
        candidate_guard(conn, args.candidate_database)
        checkpoint(conn, archive, args.candidate_database, ("migrating",))
        original_ledger_intact(conn, archive)
        if any(row["status"] in ("pending", "blocked_checksum") for row in compare_ledgers(ledger(conn))):
            raise Blocked("runner exited without completing official migrations")
        conn.execute("UPDATE refork_audit.checkpoint SET phase='migrated' WHERE baseline=%s", (BASELINE,))
    print("official migrations complete; conversion and audit are still required")


def do_convert(args):
    archive = load_archive(args.archive)
    with connect() as conn:
        candidate_guard(conn, args.candidate_database)
        phase = checkpoint(conn, archive, args.candidate_database, ("migrated", "converted"))
        ensure_no_blockers(checks(conn))
        original_ledger_intact(conn, archive)
        if any(row["status"] == "pending" for row in compare_ledgers(ledger(conn))):
            raise Blocked("official migrations must complete before conversion")
        if phase == "converted":
            if conn.execute("SELECT count(*) FROM groups WHERE platform=ANY(%s)", (list(PLATFORMS),)).fetchone()[0]:
                raise Blocked("converted checkpoint has legacy group drift")
            if conn.execute("SELECT count(*) FROM accounts WHERE platform=ANY(%s)", (list(PLATFORMS),)).fetchone()[0]:
                raise Blocked("converted checkpoint has legacy account drift")
            print("conversion already committed; run audit")
            return
        conn.execute("""
            CREATE SCHEMA IF NOT EXISTS refork_audit;
            CREATE TABLE IF NOT EXISTS refork_audit.conversions (
              baseline text NOT NULL, archive_sha256 text NOT NULL,
              entity text NOT NULL, entity_id bigint NOT NULL,
              before_row jsonb NOT NULL, after_row jsonb NOT NULL,
              converted_at timestamptz NOT NULL DEFAULT now(),
              PRIMARY KEY (baseline, entity, entity_id)
            )
        """)
        changes = validate_plan(conn, archive["plan"])
        for group_id, old, new, allowlist in changes:
            before = conn.execute("SELECT to_jsonb(g) FROM groups g WHERE id=%s FOR UPDATE", (group_id,)).fetchone()[0]
            conn.execute("UPDATE groups SET platform=%s, model_allowlist=%s::jsonb WHERE id=%s",
                         (new, safe_json(allowlist), group_id))
            after = conn.execute("SELECT to_jsonb(g) FROM groups g WHERE id=%s", (group_id,)).fetchone()[0]
            record_conversion(conn, archive, "groups", group_id, before, after)
        for account_id, old in conn.execute("SELECT id, platform FROM accounts WHERE platform=ANY(%s) ORDER BY id FOR UPDATE",
                                           (list(PLATFORMS),)):
            before = conn.execute("SELECT to_jsonb(a) FROM accounts a WHERE id=%s", (account_id,)).fetchone()[0]
            conn.execute("UPDATE accounts SET platform=%s WHERE id=%s", (PLATFORMS[old], account_id))
            after = conn.execute("SELECT to_jsonb(a) FROM accounts a WHERE id=%s", (account_id,)).fetchone()[0]
            record_conversion(conn, archive, "accounts", account_id, before, after)
        # Forward-only unfreeze; leave both legacy history and its migration intact.
        conn.execute("DROP TRIGGER IF EXISTS trg_user_subscriptions_read_only ON user_subscriptions")
        conn.execute("UPDATE refork_audit.checkpoint SET phase='converted' WHERE baseline=%s AND archive_sha256=%s",
                     (BASELINE, archive["sha256"]))
        # No quota merging: limited legacy rows already block; all unlimited rows
        # were archived before the official purge. Historical usage stays untouched.
    print("conversion committed; run audit before starting the candidate")


def record_conversion(conn, archive, entity, entity_id, before, after):
    conn.execute("""
        INSERT INTO refork_audit.conversions
          (baseline,archive_sha256,entity,entity_id,before_row,after_row)
        VALUES (%s,%s,%s,%s,%s::jsonb,%s::jsonb)
    """, (BASELINE, archive["sha256"], entity, entity_id, safe_json(before), safe_json(after)))


def do_audit(args):
    archive = load_archive(args.archive)
    with connect(read_only=True) as conn:
        current = snapshot(conn, (archive["data"].get("usage_logs") or {}).get("columns"))
        problems = []
        for table, before in archive["data"].items():
            if table in ("schema_migrations", "groups", "accounts", "user_platform_quotas"):
                continue
            after = current.get(table)
            equal = projected_rows_match(before, after or []) if isinstance(before, list) else after == before
            if not equal:
                problems.append(f"unexpected change: {table}")
        for table in ("groups", "accounts"):
            before = {row["id"]: row for row in archive["data"].get(table, [])}
            after = {row["id"]: row for row in current.get(table, [])}
            if before.keys() != after.keys():
                problems.append(f"identity set changed: {table}")
            for entity_id, row in before.items():
                actual = after.get(entity_id, {})
                expected = dict(row)
                if row["platform"] in PLATFORMS:
                    expected["platform"] = PLATFORMS[row["platform"]]
                    if table == "groups":
                        item = archive["plan"]["groups"][str(entity_id)]
                        expected["model_allowlist"] = exact_models(item["model_allowlist"])
                # New official columns may appear, but old prices/credentials must match.
                for key, value in expected.items():
                    if actual.get(key) != value:
                        problems.append(f"unexpected change: {table} id={entity_id} column={key}")
        unlimited = lambda row: all(row.get(k) is None for k in
                                   ("daily_limit_usd", "weekly_limit_usd", "monthly_limit_usd"))
        expected_quotas = [row for row in archive["data"].get("user_platform_quotas", []) if not unlimited(row)]
        if not projected_rows_match(expected_quotas, current.get("user_platform_quotas", [])):
            problems.append("unexpected quota reconciliation difference")
        comparison = compare_ledgers(ledger(conn))
        old_ledger = {r["filename"]: r["checksum"] for r in archive["data"]["schema_migrations"]}
        new_ledger = ledger(conn)
        if any(new_ledger.get(name) != value for name, value in old_ledger.items()):
            problems.append("previously applied migration records changed")
        if any(row["status"] in ("pending", "blocked_checksum") for row in comparison):
            problems.append("official migration ledger incomplete")
        frozen = conn.execute("""
            SELECT count(*) FROM pg_trigger WHERE tgname='trg_user_subscriptions_read_only'
              AND tgrelid='user_subscriptions'::regclass
        """).fetchone()[0]
        if frozen:
            problems.append("native subscriptions remain frozen")
    write_private(args.output, {"baseline": BASELINE, "archive_sha256": archive["sha256"],
                                "blockers": problems, "data_audit_passed": not problems,
                                "candidate_accepted": False})
    if problems:
        raise Blocked("; ".join(problems))
    print("data audit passed; this is not full candidate acceptance or production authorization")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    compare = sub.add_parser("compare-ledger")
    compare.add_argument("--ledger", required=True)
    compare.add_argument("--output", required=True)
    pre = sub.add_parser("preflight")
    pre.add_argument("--output", required=True)
    for name in ("archive", "migrate", "convert", "audit"):
        p = sub.add_parser(name)
        if name != "audit":
            p.add_argument("--candidate-database", required=True)
        if name == "archive":
            p.add_argument("--plan", required=True)
            p.add_argument("--output", required=True)
        else:
            p.add_argument("--archive", required=True)
        if name == "migrate":
            p.add_argument("--runner", required=True)
        if name == "audit":
            p.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        globals()["do_" + args.command.replace("-", "_")](args)
    except Blocked as exc:
        print(f"BLOCKED: {exc}", file=sys.stderr)
        return 2
    except Exception:
        # Driver errors can contain DSNs or SQL arguments. Retain diagnostics
        # locally without exposing credentials in terminal/CI output.
        print("FAILED: operation aborted; inspect private database diagnostics", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
