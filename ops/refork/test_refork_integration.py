"""Synthetic PostgreSQL rehearsal only; never reads production credentials/data."""

import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import refork
import backup


@unittest.skipUnless(os.environ.get("REFORK_TEST_DATABASE_URL"), "dedicated PostgreSQL fixture not configured")
class MigrationRehearsal(unittest.TestCase):
    def test_empty_init_upgrade_resume_conversion_and_fail_closed_audit(self):
        import psycopg
        from psycopg import sql
        from psycopg.conninfo import conninfo_to_dict, make_conninfo

        dsn = os.environ["REFORK_TEST_DATABASE_URL"]
        runner = str(Path(os.environ["REFORK_TEST_RUNNER"]).resolve())
        name = "refork_test_candidate"
        env = dict(os.environ, DATABASE_URL=dsn, REFORK_CANDIDATE_DATABASE=name, REFORK_INIT_EMPTY="true")
        env.pop("REFORK_ARCHIVE_SHA256", None)
        subprocess.run([runner], env=env, check=True)
        self.assertNotEqual(subprocess.run([runner], env=env, capture_output=True).returncode, 0)
        with psycopg.connect(dsn) as conn:
            self.assertEqual(refork.ledger(conn), refork.official_ledger())
            # Construct a pre-upgrade fixture in this disposable CI database.
            # These are NOT migration actions or instructions for a real copy.
            pending = [filename for filename in refork.official_ledger()
                       if filename.startswith(("186_alipay_", "206_", "220_", "224_", "226_", "235_", "236_", "238_", "246_"))]
            conn.execute("DELETE FROM schema_migrations WHERE filename=ANY(%s)", (pending,))
            conn.execute("ALTER TABLE groups RENAME COLUMN model_allowlist TO models_list_config")
            conn.execute("ALTER TABLE channel_monitors DROP CONSTRAINT channel_monitors_check_mode_check")
            conn.execute("DELETE FROM settings WHERE key IN ('ALIPAY_MOBILE_PRECREATE_DEEP_LINK','channel_monitor_show_quota')")
            # Reproduce a historic custom trigger, not the pinned official default.
            conn.execute("""CREATE OR REPLACE FUNCTION public.enforce_openai_long_context_billing_extra()
                RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
                IF NEW.platform='openai' AND NOT (COALESCE(NEW.extra,'{}'::jsonb) ? 'openai_long_context_billing_enabled') THEN
                    NEW.extra=jsonb_set(COALESCE(NEW.extra,'{}'::jsonb),'{openai_long_context_billing_enabled}','true'::jsonb,true);
                END IF; RETURN NEW; END $$""")
            conn.execute("""INSERT INTO schema_migrations (filename,checksum)
                VALUES ('233_backfill_and_freeze_native_subscriptions.sql',%s)""",
                         (refork.checksum("synthetic legacy fixture"),))
            conn.execute("ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check")
            user_id = conn.execute("""INSERT INTO users(email,password_hash,balance)
                VALUES ('synthetic@example.invalid','not-a-secret',123.12345678) RETURNING id""").fetchone()[0]
            legacy_providers = tuple(p for p in refork.MONITOR_PROVIDERS if p != 'opencode_go') + tuple(refork.PLATFORMS)
            for table in refork.MONITOR_TABLES:
                conn.execute(sql.SQL('ALTER TABLE {} DROP CONSTRAINT {}').format(
                    sql.Identifier(table), sql.Identifier(table + '_provider_check')))
                conn.execute(sql.SQL('ALTER TABLE {} ADD CONSTRAINT {} CHECK(provider IN ({}))').format(
                    sql.Identifier(table), sql.Identifier(table + '_provider_check'),
                    sql.SQL(',').join(map(sql.Literal, legacy_providers))))
            template_id = conn.execute("""INSERT INTO channel_monitor_request_templates(name,provider)
                VALUES ('synthetic-glm-template','glm') RETURNING id""").fetchone()[0]
            monitor_id = conn.execute("""INSERT INTO channel_monitors
                (name,provider,endpoint,api_key_encrypted,primary_model,enabled,interval_seconds,created_by,template_id)
                VALUES ('synthetic-glm-monitor','glm','https://example.invalid','synthetic-only','glm-test',false,60,%s,%s)
                RETURNING id""", (user_id, template_id)).fetchone()[0]
            conn.execute("""INSERT INTO channel_monitor_histories(monitor_id,model,status)
                VALUES (%s,'glm-test','operational')""", (monitor_id,))
            group_id = conn.execute("""INSERT INTO groups(name,platform,rate_multiplier,
                video_price_480p,video_price_720p,video_model_prices)
                VALUES ('synthetic-qwen','qwen',0.18,0,0.12345678,'{"sample":0}')
                RETURNING id""").fetchone()[0]
            account_id = conn.execute("""INSERT INTO accounts(name,platform,type,credentials,extra)
                VALUES ('synthetic-qwen','qwen','apikey',
                    '{"base_url":"https://example.invalid/v1","model_mapping":{"qwen-test":"upstream-test"}}',
                    '{"upstream_billing_manual_rate":0}')
                RETURNING id""").fetchone()[0]
            proxy_id = conn.execute("""INSERT INTO proxies(name,protocol,host,port,status)
                VALUES ('synthetic-relay','http','127.0.0.1',38480,'active') RETURNING id""").fetchone()[0]
            conn.execute("UPDATE accounts SET use_relay_route=true WHERE id=%s", (account_id,))
            for key, value in (("traffic_relay_enabled", "true"),
                               ("channel_monitor_hide_throughput", "false"),
                               ("traffic_relay_proxy_id", str(proxy_id)),
                               ("traffic_relay_unavailable_ttl_seconds", "45")):
                conn.execute("""INSERT INTO settings(key,value) VALUES (%s,%s)
                    ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value""", (key, value))
            gemini_group_id = conn.execute("""INSERT INTO groups(name,platform,allow_image_generation)
                VALUES ('synthetic-gemini','gemini',false) RETURNING id""").fetchone()[0]
            conn.execute("INSERT INTO account_groups(account_id,group_id) VALUES (%s,%s)", (account_id, group_id))
            key_id = conn.execute("""INSERT INTO api_keys(user_id,key,name,group_id)
                VALUES (%s,'synthetic-only-key','fixture',%s) RETURNING id""", (user_id, group_id)).fetchone()[0]
            conn.execute("""INSERT INTO usage_logs(user_id,api_key_id,account_id,model,total_cost,actual_cost)
                VALUES (%s,%s,%s,'qwen-test',0.1234567890,0.0222222220)""", (user_id, key_id, account_id))
            for platform in ("qwen", "openai", "minimax"):
                conn.execute("""INSERT INTO user_platform_quotas(user_id,platform,daily_usage_usd)
                    VALUES (%s,%s,1.1234567890)""", (user_id, platform))
            channel_id = conn.execute("""INSERT INTO channels(name,model_mapping)
                VALUES ('synthetic-channel','{"qwen":{"qwen-test":"upstream-test"}}') RETURNING id""").fetchone()[0]
            conn.execute("""INSERT INTO channel_model_pricing(channel_id,platform,models,input_price)
                VALUES (%s,'qwen','["qwen-test"]',0.000000123456)""", (channel_id,))
            conn.execute("""CREATE TABLE subscription_purchases
                (id bigserial PRIMARY KEY, status text NOT NULL, expires_at timestamptz NOT NULL)""")
            conn.execute("""CREATE FUNCTION fixture_subscription_freeze() RETURNS trigger LANGUAGE plpgsql AS
                $$ BEGIN RAISE EXCEPTION 'synthetic subscription freeze'; END $$""")
            conn.execute("""CREATE TRIGGER trg_user_subscriptions_read_only BEFORE INSERT OR UPDATE OR DELETE
                ON user_subscriptions FOR EACH STATEMENT EXECUTE FUNCTION fixture_subscription_freeze()""")
        env["REFORK_INIT_EMPTY"] = "false"
        env["REFORK_ARCHIVE_SHA256"] = "0" * 64
        self.assertNotEqual(subprocess.run([runner], env=env, capture_output=True).returncode, 0)
        with tempfile.TemporaryDirectory() as folder, patch.dict(os.environ, DATABASE_URL=dsn,
                REFORK_RELAY_PROOF=str(Path(folder) / "relay-proof.json")):
            args = SimpleNamespace(candidate_database=name, runner=runner)
            with refork.connect() as conn:
                conn.execute("INSERT INTO subscription_purchases(status,expires_at) VALUES ('active',now()+interval '1 day')")
            with refork.connect(read_only=True) as conn:
                self.assertIn("active legacy subscription purchases exist", refork.checks(conn)["blockers"])
            with refork.connect() as conn:
                conn.execute("DELETE FROM subscription_purchases")
            with refork.connect(read_only=True) as conn:
                self.assertIn("relay compatibility proof cannot be read", refork.checks(conn)["blockers"])
                evidence = {"baseline": refork.BASELINE, "database": name,
                    "relay_state_sha256": refork.relay_state_digest(conn), "adapter_tests_passed": True,
                    "network_test": dict.fromkeys(("test_passed", "proxy_connect", "tls_verified",
                        "origin_http_200", "direct_egress_blocked", "host_services_blocked",
                        "temporary_policy_removed", "isolation_restored"), True)}
                evidence["sha256"] = refork.archive_digest(evidence)
                refork.write_private(os.environ["REFORK_RELAY_PROOF"], evidence)
            with refork.connect(read_only=True) as conn:
                report = refork.checks(conn)
            self.assertEqual(report["blockers"], [])
            plan = {
                "archive_unlimited_quotas_before_224": True,
                "restore_video_prices_after_220": True,
                "groups": {str(group_id): {
                    "from": "qwen", "to": "openai", "account_ids": [account_id],
                    "model_allowlist": {"enabled": True, "models": ["qwen-test"]}}},
                "reviewed_cleanup": {row["filename"]: row["checksum"] for row in report["cleanup_review"]},
            }
            args.plan = str(Path(folder) / "plan.json")
            refork.write_private(args.plan, plan)
            args.dump = str(Path(folder) / "full.dump")
            args.manifest = str(Path(folder) / "backup.json")
            backup.do_create(args)
            restore_name = "refork_restore_candidate"
            with psycopg.connect(dsn, autocommit=True) as conn:
                conn.execute("CREATE DATABASE refork_restore_candidate")
            restore_dsn = make_conninfo(**dict(conninfo_to_dict(dsn), dbname=restore_name))
            args.proof = args.backup_proof = str(Path(folder) / "restore-proof.json")
            with patch.dict(os.environ, DATABASE_URL=restore_dsn):
                args.candidate_database = restore_name
                backup.do_verify(args)
            args.candidate_database = name
            args.output = args.archive = str(Path(folder) / "archive.json")
            refork.do_archive(args)
            archive = refork.load_archive(args.archive)
            original_ledger = {row["filename"]: row["checksum"] for row in archive["data"]["schema_migrations"]}
            with patch.object(refork.subprocess, "run", side_effect=subprocess.CalledProcessError(1, runner)):
                with self.assertRaises(subprocess.CalledProcessError):
                    refork.do_migrate(args)
            with refork.connect(read_only=True) as conn:
                self.assertEqual(conn.execute("SELECT phase FROM refork_audit.checkpoint").fetchone()[0], "migrating")
                self.assertEqual(conn.execute("SELECT count(*) FROM refork_audit.quota_archive").fetchone()[0], 3)
            with refork.connect() as conn:
                conn.execute("UPDATE refork_audit.quota_archive SET original_row=jsonb_set(original_row,'{daily_usage_usd}','999')")
            with self.assertRaises(refork.Blocked):
                refork.do_migrate(args)
            with refork.connect() as conn:
                for row in archive["data"]["user_platform_quotas"]:
                    conn.execute("UPDATE refork_audit.quota_archive SET original_row=%s::jsonb WHERE row_id=%s",
                                 (refork.safe_json(row), row["id"]))
            refork.do_migrate(args)
            refork.do_migrate(args)
            with refork.connect(read_only=True) as conn:
                self.assertEqual(conn.execute("SELECT count(*) FROM user_platform_quotas").fetchone()[0], 0)
                self.assertIsNone(conn.execute("SELECT video_price_480p FROM groups WHERE id=%s", (group_id,)).fetchone()[0])
            refork.do_convert(args)
            with refork.connect(read_only=True) as conn:
                self.assertIs(conn.execute("SELECT extra->'openai_long_context_billing_enabled' FROM accounts WHERE id=%s", (account_id,)).fetchone()[0], False)
            # Synthetic previous-tool fixture: a recorded default, not operator drift.
            with refork.connect() as conn:
                conn.execute("UPDATE accounts SET extra=jsonb_set(extra,'{openai_long_context_billing_enabled}','true') WHERE id=%s", (account_id,))
                conn.execute("""UPDATE refork_audit.conversions SET after_row=jsonb_set(after_row,
                    '{extra,openai_long_context_billing_enabled}','true') WHERE entity='accounts' AND entity_id=%s""", (account_id,))
                conn.execute("UPDATE accounts SET extra=jsonb_set(extra,'{unrelated_drift}','true') WHERE id=%s", (account_id,))
            with self.assertRaises(refork.Blocked):
                refork.do_convert(args)
            with refork.connect() as conn:
                conn.execute("UPDATE accounts SET extra=extra-'unrelated_drift' WHERE id=%s", (account_id,))
            refork.do_convert(args)
            refork.do_convert(args)
            with refork.connect() as conn:
                conn.execute("""UPDATE refork_audit.quota_archive
                    SET original_row=jsonb_set(original_row,'{daily_usage_usd}','999')""")
            args.output = str(Path(folder) / "blocked-audit.json")
            with self.assertRaises(refork.Blocked):
                refork.do_audit(args)
            with refork.connect() as conn:
                self.assertEqual(conn.execute("SELECT phase FROM refork_audit.checkpoint").fetchone()[0], "converted")
                for row in archive["data"]["user_platform_quotas"]:
                    conn.execute("UPDATE refork_audit.quota_archive SET original_row=%s::jsonb WHERE row_id=%s",
                                 (refork.safe_json(row), row["id"]))
            args.output = str(Path(folder) / "passed-audit.json")
            refork.do_audit(args)
            with refork.connect(read_only=True) as conn:
                self.assertEqual(conn.execute("SELECT phase FROM refork_audit.checkpoint").fetchone()[0], "audited")
                self.assertEqual(conn.execute("SELECT video_price_480p FROM groups WHERE id=%s", (group_id,)).fetchone()[0], 0)
                self.assertEqual(conn.execute("SELECT platform FROM accounts WHERE id=%s", (account_id,)).fetchone()[0], "openai")
                self.assertFalse(conn.execute("SELECT allow_image_generation FROM groups WHERE id=%s",
                                              (gemini_group_id,)).fetchone()[0])
                self.assertEqual(conn.execute("SELECT value FROM settings WHERE key='channel_monitor_hide_throughput'").fetchone()[0], "true")
                self.assertEqual(conn.execute("SELECT provider,template_id FROM channel_monitors WHERE id=%s", (monitor_id,)).fetchone(), ('zhipu', template_id))
                self.assertEqual(conn.execute("SELECT provider FROM channel_monitor_request_templates WHERE id=%s", (template_id,)).fetchone()[0], 'zhipu')
                self.assertEqual(conn.execute("SELECT count(*) FROM channel_monitor_histories WHERE monitor_id=%s", (monitor_id,)).fetchone()[0], 1)
                for filename, digest in original_ledger.items():
                    self.assertEqual(refork.ledger(conn)[filename], digest)
            args.output = str(Path(folder) / "rerun-audit.json")
            refork.do_audit(args)
            # Brand backfill is independent from legacy billing conversion.
            import group_brands
            with psycopg.connect(dsn) as conn:
                preview = group_brands.run(conn)
                ready = {g['group_id']: g['proposed_provider'] for g in preview['groups'] if g['status']=='ready'}
                self.assertEqual(ready[group_id], 'qwen')
            with psycopg.connect(dsn) as conn:
                group_brands.run(conn, apply=True)
            with psycopg.connect(dsn) as conn:
                repeated = group_brands.run(conn, apply=True)
                self.assertEqual(next(g for g in repeated['groups'] if g['group_id']==group_id)['status'], 'already_applied')
                self.assertEqual(conn.execute('SELECT platform,upstream_provider FROM groups WHERE id=%s',(group_id,)).fetchone(), ('openai','qwen'))
                self.assertEqual(conn.execute('SELECT count(*) FROM refork_audit.group_brand_updates WHERE group_id=%s',(group_id,)).fetchone()[0],1)
                self.assertEqual(conn.execute('SELECT balance FROM users WHERE id=%s',(user_id,)).fetchone()[0], __import__('decimal').Decimal('123.12345678'))


if __name__ == "__main__":
    unittest.main()
