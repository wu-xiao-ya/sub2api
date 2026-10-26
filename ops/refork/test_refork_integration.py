"""Synthetic PostgreSQL rehearsal only; never reads production credentials/data."""

import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import refork


@unittest.skipUnless(os.environ.get("REFORK_TEST_DATABASE_URL"), "dedicated PostgreSQL fixture not configured")
class MigrationRehearsal(unittest.TestCase):
    def test_empty_init_upgrade_resume_conversion_and_fail_closed_audit(self):
        import psycopg

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
                       if filename.startswith(("220_", "224_", "238_purge"))]
            conn.execute("DELETE FROM schema_migrations WHERE filename=ANY(%s)", (pending,))
            conn.execute("""INSERT INTO schema_migrations (filename,checksum)
                VALUES ('233_backfill_and_freeze_native_subscriptions.sql',%s)""",
                         (refork.checksum("synthetic legacy fixture"),))
            conn.execute("ALTER TABLE user_platform_quotas DROP CONSTRAINT user_platform_quotas_platform_check")
            user_id = conn.execute("""INSERT INTO users(email,password_hash,balance)
                VALUES ('synthetic@example.invalid','not-a-secret',123.12345678) RETURNING id""").fetchone()[0]
            group_id = conn.execute("""INSERT INTO groups(name,platform,rate_multiplier,
                video_price_480p,video_price_720p,video_model_prices)
                VALUES ('synthetic-qwen','qwen',0.18,0,0.12345678,'{"sample":0}')
                RETURNING id""").fetchone()[0]
            account_id = conn.execute("""INSERT INTO accounts(name,platform,type,credentials,extra)
                VALUES ('synthetic-qwen','qwen','apikey',
                    '{"base_url":"https://example.invalid/v1","model_mapping":{"qwen-test":"upstream-test"}}',
                    '{"upstream_billing_manual_rate":0}')
                RETURNING id""").fetchone()[0]
            conn.execute("INSERT INTO account_groups(account_id,group_id) VALUES (%s,%s)", (account_id, group_id))
            key_id = conn.execute("""INSERT INTO api_keys(user_id,key,name,group_id)
                VALUES (%s,'synthetic-only-key','fixture',%s) RETURNING id""", (user_id, group_id)).fetchone()[0]
            conn.execute("""INSERT INTO usage_logs(user_id,api_key_id,account_id,model,total_cost,actual_cost)
                VALUES (%s,%s,%s,'qwen-test',0.1234567890,0.0222222220)""", (user_id, key_id, account_id))
            for platform in ("qwen", "openai"):
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
        with tempfile.TemporaryDirectory() as folder, patch.dict(os.environ, DATABASE_URL=dsn):
            args = SimpleNamespace(candidate_database=name, runner=runner)
            with refork.connect() as conn:
                conn.execute("INSERT INTO subscription_purchases(status,expires_at) VALUES ('active',now()+interval '1 day')")
            with refork.connect(read_only=True) as conn:
                self.assertIn("active legacy subscription purchases exist", refork.checks(conn)["blockers"])
            with refork.connect() as conn:
                conn.execute("DELETE FROM subscription_purchases")
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
            args.output = args.archive = str(Path(folder) / "archive.json")
            refork.do_archive(args)
            archive = refork.load_archive(args.archive)
            original_ledger = {row["filename"]: row["checksum"] for row in archive["data"]["schema_migrations"]}
            with patch.object(refork.subprocess, "run", side_effect=subprocess.CalledProcessError(1, runner)):
                with self.assertRaises(subprocess.CalledProcessError):
                    refork.do_migrate(args)
            with refork.connect(read_only=True) as conn:
                self.assertEqual(conn.execute("SELECT phase FROM refork_audit.checkpoint").fetchone()[0], "migrating")
                self.assertEqual(conn.execute("SELECT count(*) FROM refork_audit.quota_archive").fetchone()[0], 2)
            refork.do_migrate(args)
            refork.do_migrate(args)
            with refork.connect(read_only=True) as conn:
                self.assertEqual(conn.execute("SELECT count(*) FROM user_platform_quotas").fetchone()[0], 0)
                self.assertIsNone(conn.execute("SELECT video_price_480p FROM groups WHERE id=%s", (group_id,)).fetchone()[0])
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
                for filename, digest in original_ledger.items():
                    self.assertEqual(refork.ledger(conn)[filename], digest)
            args.output = str(Path(folder) / "rerun-audit.json")
            refork.do_audit(args)


if __name__ == "__main__":
    unittest.main()
