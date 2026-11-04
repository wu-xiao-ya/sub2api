#!/usr/bin/env python3
"""Fail-closed candidate migration coordinator; DATABASE_URL is never logged."""

import argparse
import hashlib
import simplejson as json
import os
from pathlib import Path
import re
import subprocess
import sys
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal
from functools import partial

if __name__ == "__main__":
    # backup.py also imports these shared guard helpers when this is a CLI entry.
    sys.modules["refork"] = sys.modules[__name__]

BASELINE = "ac1b8f1271be0d674fa6f074403c1410f30a3a81"
ROOT = Path(__file__).resolve().parents[2]
MIGRATIONS = ROOT / "backend" / "migrations"
LOCK_ID = 694208311321144027
PLATFORMS = {"glm": "zhipu", "qwen": "openai", "mimo": "openai", "hunyuan": "openai"}
MONITOR_TABLES = ("channel_monitors", "channel_monitor_request_templates")
MONITOR_PROVIDERS = ("openai", "anthropic", "gemini", "grok", "antigravity",
                     "kimi", "zhipu", "deepseek", "minimax", "opencode_go")
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


def convert_channel_mapping(mapping):
    if mapping is None:
        return None
    if not isinstance(mapping, dict) or any(not isinstance(v, dict) for v in mapping.values()):
        raise Blocked("channel model_mapping has an unsupported shape")
    result = {}
    sources = {}
    for platform, models in mapping.items():
        target = PLATFORMS.get(platform, platform)
        sources.setdefault(target, []).append(platform)
        merged = result.setdefault(target, {})
        for name, upstream in models.items():
            if not isinstance(name, str) or not isinstance(upstream, str):
                raise Blocked("channel mappings must map model strings to strings")
            for existing, previous in merged.items():
                if name.lower() == existing.lower() and upstream != previous:
                    raise Blocked("channel model mapping collision after platform conversion")
            merged[name] = upstream
    for platform, origins in sources.items():
        if len(origins) > 1 and any("*" in key for key in result[platform]):
            raise Blocked("merged channel wildcard precedence requires explicit review")
    return result


def validate_pricing_platforms(rows, owner):
    seen = {}
    platforms = {}
    wildcard = set()
    for row in rows:
        old = row["platform"]
        target = PLATFORMS.get(old, old)
        bucket = (row[owner], target)
        platforms.setdefault(bucket, set()).add(old)
        for model in row["models"] or []:
            key = (bucket, model.strip().lower())
            previous = seen.get(key)
            if previous is not None and previous != old:
                raise Blocked("channel pricing model collision after platform conversion")
            seen[key] = old
            if "*" in model:
                wildcard.add(bucket)
    if any(len(platforms[bucket]) > 1 for bucket in wildcard):
        raise Blocked("merged wildcard channel pricing requires explicit review")


def safe_json(value):
    return json.dumps(value, default=str, use_decimal=True, sort_keys=True, separators=(",", ":"), ensure_ascii=True)


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
        stream.write(json.dumps(data, default=str, use_decimal=True, indent=2, sort_keys=True) + "\n")


def table_exists(conn, name):
    return conn.execute("SELECT to_regclass(%s) IS NOT NULL", (f"public.{name}",)).fetchone()[0]


def historical_table_fingerprints(conn, excluded, original=None):
    from psycopg import sql
    from backup import primary_key_columns

    if original is None:
        names = [row[0] for row in conn.execute(
            "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
                 if row[0] not in excluded]
        original = {}
        for name in names:
            columns = [row[0] for row in conn.execute("""
                SELECT column_name FROM information_schema.columns
                WHERE table_schema='public' AND table_name=%s ORDER BY ordinal_position
            """, (name,))]
            original[name] = {"columns": columns, "order_by": primary_key_columns(conn, name)}
    result = {}
    for index, (name, evidence) in enumerate(sorted(original.items())):
        if not table_exists(conn, name):
            result[name] = None
            continue
        digest = hashlib.sha256()
        count = 0
        columns = evidence["columns"]
        order = evidence["order_by"]
        sort = (sql.SQL(",").join(map(sql.Identifier, order)) if order
                else sql.SQL('to_jsonb(p)::text COLLATE "C"'))
        query = sql.SQL('SELECT to_jsonb(p)::text FROM (SELECT {} FROM public.{}) p ORDER BY {}').format(
            sql.SQL(",").join(map(sql.Identifier, columns)), sql.Identifier(name), sort)
        with conn.cursor(name=f"refork_history_{index}") as cursor:
            cursor.execute(query)
            for (row,) in cursor:
                digest.update(row.encode("utf-8") + b"\n")
                count += 1
        result[name] = {"count": count, "sha256": digest.hexdigest(), "columns": columns, "order_by": order}
    return result


def snapshot(conn, usage_columns=None, historical_tables=None):
    """Capture configuration/identity evidence locally; never upload this to CI."""
    tables = (
        "users", "api_keys", "accounts", "groups", "account_groups",
        "user_allowed_groups", "user_group_rates", "user_platform_quotas",
        "user_subscriptions", "subscription_purchases", "subscription_purchase_groups",
        "subscription_plans", "subscription_plan_groups", "group_promotions",
        "account_contribution_logs", "payment_orders", "payment_audit_logs",
        "user_affiliate_ledger", "redeem_codes", "channels", "channel_groups",
        "channel_model_pricing", "channel_account_stats_model_pricing", "composite_model_routes",
        "auth_cache_invalidation_outbox",
        "channel_monitors", "channel_monitor_request_templates",
    )
    evidence = {}
    for name in tables:
        if table_exists(conn, name):
            # Some join tables have composite primary keys and no id.
            data = [json.loads(safe_json(row[0]), use_decimal=True) for row in conn.execute(f"SELECT to_jsonb(t) FROM public.{name} t")]
            evidence[name] = sorted(data, key=safe_json)
    evidence["schema_migrations"] = [dict(zip(("filename", "checksum"), row)) for row in conn.execute(
        "SELECT filename, checksum FROM schema_migrations ORDER BY filename")]
    # Settings can contain provider credentials. They stay inside the private archive.
    if table_exists(conn, "settings"):
        evidence["settings"] = [json.loads(safe_json(row[0]), use_decimal=True) for row in conn.execute(
            "SELECT to_jsonb(t) FROM settings t ORDER BY key")]
    evidence["usage_logs"] = usage_fingerprint(conn, usage_columns)
    evidence["other_tables"] = historical_table_fingerprints(
        conn, set(tables) | {"settings", "usage_logs", "schema_migrations"}, historical_tables)
    return evidence


def projected_rows_match(before, after):
    """Official migrations may add columns, but cannot rewrite original fields."""
    if not before:
        return not after
    columns = set(before[0])
    if any(set(row) != columns for row in before):
        raise Blocked("inconsistent archived row columns")
    if any(not columns.issubset(row) for row in after):
        return False
    projected = [{key: row.get(key) for key in columns} for row in after]
    return sorted(before, key=safe_json) == sorted(projected, key=safe_json)


def invalidation_rows_match(before, after, api_keys):
    previous = {row["id"]: row for row in before}
    old_rows = [row for row in after if row["id"] in previous]
    if not projected_rows_match(before, old_rows):
        return False
    known_keys = {hashlib.sha256(row["key"].encode("utf-8")).hexdigest()
                  for row in api_keys if row.get("key")}
    return all(row.get("cache_key", "").strip() in known_keys
               for row in after if row["id"] not in previous)


def expected_account_extra(row):
    extra = dict(row.get("extra") or {})
    # A missing legacy flag must not acquire an OpenAI long-context surcharge.
    # Historic custom triggers may default it to true; seed false explicitly.
    if row["platform"] in ("qwen", "mimo", "hunyuan"):
        extra.setdefault("openai_long_context_billing_enabled", False)
    return extra


def expected_group_fields(row, actual):
    expected = dict(row)
    # Official 235/236 rename the legacy column; its value must still match.
    if "models_list_config" in expected and "models_list_config" not in actual:
        expected["model_allowlist"] = expected.pop("models_list_config")
    return expected


def settings_rows_match(before, after, plan, ledger=()):
    name = "206_channel_monitor_v2_privacy_defaults.sql"
    reviewed = plan.get("reviewed_cleanup", {}).get(name) == checksum((MIGRATIONS / name).read_bytes())
    actual = {row["key"]: row for row in after}
    if len(actual) != len(after):
        return False
    old_keys = {row["key"] for row in before}
    additions = {
        "ALIPAY_MOBILE_PRECREATE_DEEP_LINK": "186_alipay_mobile_precreate_deep_link.sql",
        "channel_monitor_show_quota": "226_channel_monitor_quota_mode.sql",
    }
    pending = {row["filename"]: row.get("file_checksum") for row in ledger if row["status"] == "pending"}
    for key in actual.keys() - old_keys:
        migration = additions.get(key)
        if (not migration or actual[key].get("value") != "false"
                or pending.get(migration) != checksum((MIGRATIONS / migration).read_bytes())):
            return False
    for row in before:
        current = actual.get(row["key"], {})
        expected = dict(row)
        # Exact reviewed official privacy correction; never a pricing exception.
        if (reviewed and row["key"] == "channel_monitor_hide_throughput"
                and str(row.get("value", "")).strip().lower() in ("false", "0", "off", "disabled")
                and current.get("value") == "true"):
            expected["value"] = "true"
            if "updated_at" in expected and "updated_at" in current:
                expected["updated_at"] = current["updated_at"]
        if any(key not in current or current[key] != value for key, value in expected.items()):
            return False
    return True


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


def relay_state_digest(conn):
    accounts = list(conn.execute("SELECT id,use_relay_route,proxy_id FROM accounts WHERE use_relay_route ORDER BY id"))
    settings = dict(conn.execute("SELECT key,value FROM settings WHERE key=ANY(%s) ORDER BY key",
                                (["traffic_relay_enabled", "traffic_relay_proxy_id",
                                  "traffic_relay_unavailable_ttl_seconds"],)))
    try:
        proxy_id = int(settings.get("traffic_relay_proxy_id", "0"))
    except (ValueError, TypeError):
        raise Blocked("relay proxy setting is invalid")
    row = conn.execute("SELECT to_jsonb(p) FROM proxies p WHERE id=%s", (proxy_id,)).fetchone()
    if settings.get("traffic_relay_enabled") != "true" or not row:
        raise Blocked("relay opt-ins require an enabled proxy configuration")
    proxy = row[0]
    if proxy.get("protocol") != "http" or proxy.get("status") != "active":
        raise Blocked("relay opt-ins require an active HTTP proxy")
    return hashlib.sha256(safe_json({"accounts": accounts, "settings": settings,
                                    "proxy": proxy}).encode("utf-8")).hexdigest()


def validate_relay_evidence(proof, database, state_digest):
    if "candidate" not in database.lower() or proof.get("database") != database:
        raise Blocked("relay compatibility proof database mismatch")
    if proof.get("baseline") != BASELINE or proof.get("relay_state_sha256") != state_digest:
        raise Blocked("relay compatibility proof configuration mismatch")
    if proof.get("adapter_tests_passed") is not True:
        raise Blocked("relay adapter tests are not verified")
    required = ("test_passed", "proxy_connect", "tls_verified", "origin_http_200",
                "direct_egress_blocked", "host_services_blocked",
                "temporary_policy_removed", "isolation_restored")
    if any(proof.get("network_test", {}).get(key) is not True for key in required):
        raise Blocked("controlled candidate relay network test is incomplete")


def relay_compatibility(conn):
    path = os.environ.get("REFORK_RELAY_PROOF")
    if not path:
        raise Blocked("legacy relay opt-ins require a verified egress adapter before migration")
    try:
        proof = load_archive(path)
    except (OSError, ValueError):
        raise Blocked("relay compatibility proof cannot be read")
    database = conn.execute("SELECT current_database()").fetchone()[0]
    digest = relay_state_digest(conn)
    validate_relay_evidence(proof, database, digest)
    return {"proof_sha256": proof["sha256"], "relay_state_sha256": digest}


def checks(conn):
    problems = []
    relay = None
    comparison = compare_ledgers(ledger(conn))
    problems += [f"checksum mismatch: {r['filename']}" for r in comparison
                 if r["status"] == "blocked_checksum"]
    if conn.execute("""SELECT EXISTS (SELECT 1 FROM information_schema.columns
        WHERE table_schema='public' AND table_name='accounts' AND column_name='use_relay_route')""").fetchone()[0]:
        if conn.execute("SELECT EXISTS (SELECT 1 FROM accounts WHERE use_relay_route)").fetchone()[0]:
            try:
                relay = relay_compatibility(conn)
            except Blocked as exc:
                problems.append(str(exc))
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
        if any(r["filename"] == "224_user_platform_quotas_add_cn_providers.sql" and r["status"] == "pending"
               for r in comparison):
            count = conn.execute("""SELECT count(*) FROM user_platform_quotas
                WHERE platform<>ALL(%s) AND (daily_limit_usd IS NOT NULL
                    OR weekly_limit_usd IS NOT NULL OR monthly_limit_usd IS NOT NULL)""",
                (["anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek"],)).fetchone()[0]
            if count:
                problems.append("limited platform quotas conflict with pending migration 224")
    pending = {r["filename"] for r in comparison if r["status"] == "pending"}
    for table in MONITOR_TABLES:
        if table_exists(conn, table):
            providers = [row[0] for row in conn.execute(f"SELECT DISTINCT provider FROM {table}")]
            if any(PLATFORMS.get(value, value) not in MONITOR_PROVIDERS for value in providers):
                problems.append(f"unsupported monitor provider in {table}")
    if table_exists(conn, "channel_monitor_request_templates"):
        seen = set()
        for provider, name in conn.execute("SELECT provider,name FROM channel_monitor_request_templates"):
            target = (PLATFORMS.get(provider, provider), name)
            if target in seen:
                problems.append("monitor template provider conversion has a name collision")
                break
            seen.add(target)
    for table, column in (("composite_model_routes", "target_platform"),):
        if table_exists(conn, table):
            count = conn.execute(f"SELECT count(*) FROM {table} WHERE {column}=ANY(%s)",
                                 (list(PLATFORMS),)).fetchone()[0]
            if count:
                problems.append(f"legacy platform references in {table} require reviewed routing conversion")
    for table, owner in (("channel_model_pricing", "channel_id"),
                         ("channel_account_stats_model_pricing", "rule_id")):
        if table_exists(conn, table):
            rows = [row[0] for row in conn.execute(f"SELECT to_jsonb(t) FROM {table} t")]
            try:
                validate_pricing_platforms(rows, owner)
            except Blocked as exc:
                problems.append(f"{table}: {exc}")
    if table_exists(conn, "channels"):
        for channel_id, mapping in conn.execute("SELECT id, model_mapping FROM channels"):
            try:
                convert_channel_mapping(mapping)
            except Blocked as exc:
                problems.append(f"channel {channel_id}: {exc}")
    cleanup = []
    for name in sorted(pending):
        text = (MIGRATIONS / name).read_text(encoding="utf-8")
        # This is a review inventory, not an SQL parser or automatic approval.
        if re.search(r"\b(DELETE\s+FROM|DROP|TRUNCATE|UPDATE)\b", text, re.I):
            cleanup.append({"filename": name, "checksum": checksum(text)})
    return {"baseline": BASELINE, "applied_count": len(ledger(conn)),
            "ledger": comparison, "cleanup_review": cleanup, "blockers": problems,
            "relay_compatibility": relay}


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
    conn.execute("SET TIME ZONE 'UTC'")
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
    archive = json.loads(Path(path).read_text(encoding="utf-8"), use_decimal=True)
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
    import backup

    with connect() as conn:
        conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
        candidate_guard(conn, args.candidate_database)
        conn.execute("LOCK TABLE groups, accounts, user_subscriptions IN SHARE ROW EXCLUSIVE MODE")
        report = checks(conn)
        ensure_no_blockers(report)
        plan = json.loads(Path(args.plan).read_text(encoding="utf-8"), use_decimal=True)
        if plan.get("archive_unlimited_quotas_before_224") is not True:
            raise Blocked("explicit archive-first quota preparation approval is required")
        if plan.get("restore_video_prices_after_220") is not True:
            raise Blocked("explicit video price preservation approval is required")
        validate_plan(conn, plan)
        backup_proof = backup.validate_proof(args.backup_proof, conn, args.candidate_database)
        approved = plan.get("reviewed_cleanup", {})
        for item in report["cleanup_review"]:
            if approved.get(item["filename"]) != item["checksum"]:
                raise Blocked(f"pending data-changing migration needs review: {item['filename']}")
        data = snapshot(conn)
        archive = {"baseline": BASELINE, "database": args.candidate_database,
                   "created_at": datetime.now(timezone.utc).isoformat(), "report": report,
                   "plan": plan, "data": data, "backup_proof": backup_proof}
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
            DO NOTHING
        """, (BASELINE, archive["sha256"]))
        existing = conn.execute("SELECT archive_sha256 FROM refork_audit.checkpoint WHERE baseline=%s",
                                (BASELINE,)).fetchone()[0]
        if existing != archive["sha256"]:
            raise Blocked("candidate already has another archive; retain the original checkpoint")
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
            if snapshot(conn, usage_columns, archive["data"].get("other_tables")) != archive["data"]:
                raise Blocked("candidate data drift since archive; restore a new candidate, do not replace its checkpoint")
            conn.execute("""
                CREATE TABLE IF NOT EXISTS refork_audit.quota_archive (
                  baseline text NOT NULL, archive_sha256 text NOT NULL,
                  row_id bigint NOT NULL, original_row jsonb NOT NULL,
                  PRIMARY KEY (baseline, row_id)
                )
            """)
            if table_exists(conn, "user_platform_quotas"):
                conn.execute("""
                    INSERT INTO refork_audit.quota_archive (baseline,archive_sha256,row_id,original_row)
                    SELECT %s,%s,id,to_jsonb(q) FROM user_platform_quotas q
                    WHERE daily_limit_usd IS NULL AND weekly_limit_usd IS NULL AND monthly_limit_usd IS NULL
                    ON CONFLICT DO NOTHING
                """, (BASELINE, archive["sha256"]))
        # Also run on interrupted migrations: 224 rejects e.g. minimax before
        # the later official expansion. Remove only verified archived unlimited
        # rows, using the final official unlimited-quota semantics early.
        prepare_unlimited_quotas(conn, archive)
        prepare_monitor_providers(conn, archive)
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


def prepare_unlimited_quotas(conn, archive):
    if not table_exists(conn, "user_platform_quotas"):
        return
    unlimited = lambda row: all(row.get(key) is None for key in
                               ("daily_limit_usd", "weekly_limit_usd", "monthly_limit_usd"))
    original = archive["data"].get("user_platform_quotas", [])
    expected = [row for row in original if unlimited(row)]
    archived = [row[0] for row in conn.execute("""SELECT original_row FROM refork_audit.quota_archive
        WHERE baseline=%s AND archive_sha256=%s ORDER BY row_id""", (BASELINE, archive["sha256"]))]
    if sorted(expected, key=safe_json) != sorted(archived, key=safe_json):
        raise Blocked("quota archive must match before any unlimited quota removal")
    known = {row["id"]: row for row in original}
    current = [row[0] for row in conn.execute("SELECT to_jsonb(q) FROM user_platform_quotas q ORDER BY id FOR UPDATE")]
    for row in current:
        if row["id"] not in known or not projected_rows_match([known[row["id"]]], [row]):
            raise Blocked("quota data drift before unlimited quota preparation")
    # Limited rows must neither disappear nor change across recovery attempts.
    ids = {row["id"] for row in current}
    if any(row["id"] not in ids for row in original if not unlimited(row)):
        raise Blocked("limited quota row disappeared before migration")
    remove = [row["id"] for row in current if unlimited(row)]
    if remove:
        conn.execute("""DELETE FROM user_platform_quotas WHERE id=ANY(%s)
            AND daily_limit_usd IS NULL AND weekly_limit_usd IS NULL AND monthly_limit_usd IS NULL""", (remove,))


def prepare_monitor_providers(conn, archive):
    from psycopg import sql

    for table in MONITOR_TABLES:
        if not table_exists(conn, table):
            continue
        current = [r[0] for r in conn.execute(f"SELECT to_jsonb(t) FROM {table} t ORDER BY id FOR UPDATE")]
        original = archive["data"].get(table)
        if original is None:
            if any(row["provider"] in PLATFORMS for row in current):
                raise Blocked("legacy monitor providers require a fresh structured archive from the verified backup")
            continue
        if not any(row["provider"] in PLATFORMS for row in original):
            continue
        before = {row["id"]: row for row in original}
        if set(before) != {row["id"] for row in current}:
            raise Blocked("monitor identities changed before provider preparation")
        for row in current:
            expected = dict(before[row["id"]])
            if row["provider"] == PLATFORMS.get(expected["provider"], expected["provider"]):
                expected["provider"] = row["provider"]
            if not projected_rows_match([expected], [row]):
                raise Blocked("monitor data drift before provider preparation")
        # One transaction: preserve every row/ID/FK, translate the provider and
        # validate the exact final official 238 whitelist. No check stays off.
        constraint = table + "_provider_check"
        conn.execute(sql.SQL("ALTER TABLE {} DROP CONSTRAINT IF EXISTS {}").format(
            sql.Identifier(table), sql.Identifier(constraint)))
        conn.execute("""CREATE TABLE IF NOT EXISTS refork_audit.preparations (
            baseline text NOT NULL,archive_sha256 text NOT NULL,entity text NOT NULL,
            entity_id bigint NOT NULL,before_row jsonb NOT NULL,after_row jsonb NOT NULL,
            PRIMARY KEY (baseline,entity,entity_id))""")
        for row in original:
            target = PLATFORMS.get(row["provider"], row["provider"])
            if target == row["provider"]:
                continue
            after = dict(row, provider=target)
            conn.execute("""INSERT INTO refork_audit.preparations
                (baseline,archive_sha256,entity,entity_id,before_row,after_row)
                VALUES (%s,%s,%s,%s,%s::jsonb,%s::jsonb) ON CONFLICT DO NOTHING""",
                (BASELINE, archive["sha256"], table, row["id"], safe_json(row), safe_json(after)))
            saved = conn.execute("""SELECT before_row,after_row FROM refork_audit.preparations
                WHERE baseline=%s AND archive_sha256=%s AND entity=%s AND entity_id=%s""",
                (BASELINE, archive["sha256"], table, row["id"])).fetchone()
            if saved is None or saved != (row, after):
                raise Blocked("monitor provider preparation trace mismatch")
            conn.execute(sql.SQL("UPDATE {} SET provider=%s WHERE id=%s").format(sql.Identifier(table)),
                         (target, row["id"]))
        conn.execute(sql.SQL("ALTER TABLE {} ADD CONSTRAINT {} CHECK(provider IN ({}))").format(
            sql.Identifier(table), sql.Identifier(constraint),
            sql.SQL(",").join(map(sql.Literal, MONITOR_PROVIDERS))))


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
            repair_converted_long_context_defaults(conn, archive)
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
            if old in ("qwen", "mimo", "hunyuan"):
                original = next(row for row in archive["data"]["accounts"] if row["id"] == account_id)
                flag = expected_account_extra(original)["openai_long_context_billing_enabled"]
                if not isinstance(flag, bool):
                    raise Blocked("legacy account has a non-boolean long-context billing flag")
                conn.execute("""UPDATE accounts SET platform=%s,
                    extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_long_context_billing_enabled}',%s::jsonb,true)
                    WHERE id=%s""", (PLATFORMS[old], safe_json(flag), account_id))
            else:
                conn.execute("UPDATE accounts SET platform=%s WHERE id=%s", (PLATFORMS[old], account_id))
            after = conn.execute("SELECT to_jsonb(a) FROM accounts a WHERE id=%s", (account_id,)).fetchone()[0]
            record_conversion(conn, archive, "accounts", account_id, before, after)
        for table in ("channel_model_pricing", "channel_account_stats_model_pricing"):
            if not table_exists(conn, table):
                continue
            for entity_id, old in conn.execute(
                    f"SELECT id,platform FROM {table} WHERE platform=ANY(%s) ORDER BY id FOR UPDATE",
                    (list(PLATFORMS),)).fetchall():
                before = conn.execute(f"SELECT to_jsonb(t) FROM {table} t WHERE id=%s", (entity_id,)).fetchone()[0]
                conn.execute(f"UPDATE {table} SET platform=%s WHERE id=%s", (PLATFORMS[old], entity_id))
                after = conn.execute(f"SELECT to_jsonb(t) FROM {table} t WHERE id=%s", (entity_id,)).fetchone()[0]
                record_conversion(conn, archive, table, entity_id, before, after)
        if table_exists(conn, "channels"):
            for entity_id, mapping in conn.execute("SELECT id,model_mapping FROM channels ORDER BY id FOR UPDATE").fetchall():
                converted = convert_channel_mapping(mapping)
                if mapping == converted:
                    continue
                before = conn.execute("SELECT to_jsonb(t) FROM channels t WHERE id=%s", (entity_id,)).fetchone()[0]
                conn.execute("UPDATE channels SET model_mapping=%s::jsonb WHERE id=%s", (safe_json(converted), entity_id))
                after = conn.execute("SELECT to_jsonb(t) FROM channels t WHERE id=%s", (entity_id,)).fetchone()[0]
                record_conversion(conn, archive, "channels", entity_id, before, after)
        for original in archive["data"].get("groups", []):
            # Official 246 enables images broadly. Preserve operator permissions.
            if "allow_image_generation" in original:
                before = conn.execute("SELECT allow_image_generation FROM groups WHERE id=%s FOR UPDATE",
                                      (original["id"],)).fetchone()[0]
                after = original["allow_image_generation"]
                if before != after:
                    record_conversion(conn, archive, "group_image_permission", original["id"],
                                      {"allow_image_generation": before}, {"allow_image_generation": after})
                    conn.execute("UPDATE groups SET allow_image_generation=%s WHERE id=%s",
                                 (after, original["id"]))
            # Restore exactly the original values, including explicit free prices.
            fields = ("video_price_480p", "video_price_720p", "video_price_1080p", "video_model_prices")
            if all(key in original for key in fields):
                before = conn.execute("""SELECT jsonb_build_object(
                    'video_price_480p',video_price_480p,'video_price_720p',video_price_720p,
                    'video_price_1080p',video_price_1080p,'video_model_prices',video_model_prices)
                    FROM groups WHERE id=%s""", (original["id"],)).fetchone()[0]
                after = {field: original[field] for field in fields}
                if before != after:
                    record_conversion(conn, archive, "group_video_prices", original["id"], before, after)
                conn.execute("""
                    UPDATE groups SET video_price_480p=%s,video_price_720p=%s,
                        video_price_1080p=%s,video_model_prices=%s::jsonb WHERE id=%s
                """, (original[fields[0]], original[fields[1]], original[fields[2]],
                      None if original[fields[3]] is None else safe_json(original[fields[3]]), original["id"]))
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


def repair_converted_long_context_defaults(conn, archive):
    key = "openai_long_context_billing_enabled"
    for original in archive["data"].get("accounts", []):
        if original["platform"] not in ("qwen", "mimo", "hunyuan") or key in (original.get("extra") or {}):
            continue
        row = conn.execute("SELECT to_jsonb(a) FROM accounts a WHERE id=%s FOR UPDATE", (original["id"],)).fetchone()
        if row is None:
            raise Blocked("converted account disappeared during billing-flag recovery")
        actual = row[0]
        if actual.get("extra", {}).get(key) is False:
            continue
        saved = conn.execute("""SELECT before_row,after_row FROM refork_audit.conversions
            WHERE baseline=%s AND archive_sha256=%s AND entity='accounts' AND entity_id=%s""",
            (BASELINE, archive["sha256"], original["id"])).fetchone()
        if (actual.get("platform") != "openai" or actual.get("extra", {}).get(key) is not True
                or saved is None or saved[1] != actual
                or not projected_rows_match([original], [saved[0]])):
            raise Blocked("converted account drift blocks billing-flag recovery")
        conn.execute("""UPDATE accounts SET extra=jsonb_set(extra,
            '{openai_long_context_billing_enabled}','false'::jsonb,true) WHERE id=%s""", (original["id"],))
        flag = conn.execute("SELECT extra->'openai_long_context_billing_enabled' FROM accounts WHERE id=%s",
                            (original["id"],)).fetchone()[0]
        if flag is not False:
            raise Blocked("account trigger prevents preserving legacy long-context billing")
        record_conversion(conn, archive, "account_long_context_compat", original["id"], {key: True}, {key: False})


def do_audit(args):
    archive = load_archive(args.archive)
    with connect() as conn:
        conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
        candidate_guard(conn, args.candidate_database)
        checkpoint(conn, archive, args.candidate_database, ("converted", "audited"))
        original_ledger_intact(conn, archive)
        current = snapshot(conn, (archive["data"].get("usage_logs") or {}).get("columns"),
                           archive["data"].get("other_tables"))
        problems = [f"historical table disappeared: {name}"
                    for name in archive["data"] if name not in current]
        for table, before in archive["data"].items():
            if table in ("schema_migrations", "groups", "accounts", "user_platform_quotas",
                         "channels", "channel_model_pricing", "channel_account_stats_model_pricing",
                         "channel_monitors", "channel_monitor_request_templates"):
                continue
            after = current.get(table)
            if table == "auth_cache_invalidation_outbox":
                equal = invalidation_rows_match(before, after or [], archive["data"].get("api_keys", []))
            elif table == "settings":
                equal = settings_rows_match(before, after or [], archive["plan"], archive["report"]["ledger"])
            else:
                equal = projected_rows_match(before, after or []) if isinstance(before, list) else after == before
            if not equal:
                if table == "other_tables":
                    problems += [f"unexpected historical table change: {name}"
                                 for name in before if before[name] != (after or {}).get(name)]
                else:
                    problems.append(f"unexpected change: {table}")
        for table in ("groups", "accounts"):
            before = {row["id"]: row for row in archive["data"].get(table, [])}
            after = {row["id"]: row for row in current.get(table, [])}
            if before.keys() != after.keys():
                problems.append(f"identity set changed: {table}")
            for entity_id, row in before.items():
                actual = after.get(entity_id, {})
                expected = expected_group_fields(row, actual) if table == "groups" else dict(row)
                if row["platform"] in PLATFORMS:
                    expected["platform"] = PLATFORMS[row["platform"]]
                    if table == "groups":
                        item = archive["plan"]["groups"][str(entity_id)]
                        expected["model_allowlist"] = exact_models(item["model_allowlist"])
                    else:
                        expected["extra"] = expected_account_extra(row)
                # New official columns may appear, but old prices/credentials must match.
                for key, value in expected.items():
                    if key not in actual or actual[key] != value:
                        problems.append(f"unexpected change: {table} id={entity_id} column={key}")
        unlimited = lambda row: all(row.get(k) is None for k in
                                   ("daily_limit_usd", "weekly_limit_usd", "monthly_limit_usd"))
        expected_quotas = [row for row in archive["data"].get("user_platform_quotas", []) if not unlimited(row)]
        if not projected_rows_match(expected_quotas, current.get("user_platform_quotas", [])):
            problems.append("unexpected quota reconciliation difference")
        archived_quotas = [row[0] for row in conn.execute("""
            SELECT original_row FROM refork_audit.quota_archive
            WHERE baseline=%s AND archive_sha256=%s ORDER BY row_id
        """, (BASELINE, archive["sha256"]))]
        expected_archive = [row for row in archive["data"].get("user_platform_quotas", []) if unlimited(row)]
        if sorted(expected_archive, key=safe_json) != sorted(archived_quotas, key=safe_json):
            problems.append("unlimited quota archive missing or changed")
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
        for table in ("channels", "channel_model_pricing", "channel_account_stats_model_pricing"):
            before = archive["data"].get(table, [])
            expected = []
            for row in before:
                changed = dict(row)
                if table == "channels":
                    changed["model_mapping"] = convert_channel_mapping(row.get("model_mapping"))
                else:
                    changed["platform"] = PLATFORMS.get(row["platform"], row["platform"])
                expected.append(changed)
            if not projected_rows_match(expected, current.get(table, [])):
                problems.append(f"routing or price difference: {table}")
        for table in MONITOR_TABLES:
            expected = [dict(row, provider=PLATFORMS.get(row["provider"], row["provider"]))
                        for row in archive["data"].get(table, [])]
            if not projected_rows_match(expected, current.get(table, [])):
                problems.append(f"monitor provider, identity or configuration difference: {table}")
        write_private(args.output, {"baseline": BASELINE, "archive_sha256": archive["sha256"],
                                    "blockers": problems, "data_audit_passed": not problems,
                                    "candidate_accepted": False})
        if not problems:
            conn.execute("UPDATE refork_audit.checkpoint SET phase='audited' WHERE baseline=%s", (BASELINE,))
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
        p.add_argument("--candidate-database", required=True)
        if name == "archive":
            p.add_argument("--plan", required=True)
            p.add_argument("--backup-proof", required=True)
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
