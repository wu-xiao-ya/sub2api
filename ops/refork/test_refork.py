import unittest
import tempfile
from pathlib import Path

import refork


class LedgerTests(unittest.TestCase):
    def test_only_reviewed_official_privacy_default_can_change_settings(self):
        name = "206_channel_monitor_v2_privacy_defaults.sql"
        plan = {"reviewed_cleanup": {name: refork.checksum((refork.MIGRATIONS / name).read_bytes())}}
        before = [{"key": "channel_monitor_hide_throughput", "value": "false", "updated_at": "old"},
                  {"key": "currency", "value": "USD", "updated_at": "old"}]
        after = [dict(before[0], value="true", updated_at="new"), before[1]]
        self.assertTrue(refork.settings_rows_match(before, after, plan))
        self.assertFalse(refork.settings_rows_match(before, after, {}))
        self.assertFalse(refork.settings_rows_match(before, [after[0], dict(after[1], value="CNY")], plan))
        self.assertFalse(refork.settings_rows_match(before, after[:1], plan))

    def test_relay_evidence_is_bound_to_candidate_and_configuration(self):
        flags = ("test_passed", "proxy_connect", "tls_verified", "origin_http_200",
                 "direct_egress_blocked", "host_services_blocked",
                 "temporary_policy_removed", "isolation_restored")
        proof = {"baseline": refork.BASELINE, "database": "copy_candidate",
                 "relay_state_sha256": "a" * 64, "adapter_tests_passed": True,
                 "network_test": dict.fromkeys(flags, True)}
        refork.validate_relay_evidence(proof, "copy_candidate", "a" * 64)
        for database, digest in (("other_candidate", "a" * 64), ("production", "a" * 64),
                                 ("copy_candidate", "b" * 64)):
            with self.assertRaises(refork.Blocked):
                refork.validate_relay_evidence(proof, database, digest)
        for key in flags:
            changed = dict(proof, network_test=dict(proof["network_test"], **{key: False}))
            with self.assertRaises(refork.Blocked):
                refork.validate_relay_evidence(changed, "copy_candidate", "a" * 64)
        with self.assertRaises(refork.Blocked):
            refork.validate_relay_evidence(dict(proof, adapter_tests_passed=False), "copy_candidate", "a" * 64)

    def test_trimspace_matches_go_not_python_c0(self):
        self.assertEqual(refork.checksum("\u2000SELECT 1;\r\n"), refork.checksum("SELECT 1;"))
        self.assertNotEqual(refork.checksum("\x1cSELECT 1;"), refork.checksum("SELECT 1;"))

    def test_full_filename_not_prefix(self):
        result = refork.compare_ledgers(
            {"238_custom.sql": "old"}, {"238_official.sql": "new"}, {})
        self.assertEqual([r["status"] for r in result], ["legacy_only", "pending"])

    def test_compatibility_requires_both_known_checksums_and_exact_filename(self):
        rules = {"same.sql": {"old", "new"}}
        for old, new, status in (
            ("old", "new", "compatible"),
            ("unknown", "new", "blocked_checksum"),
            ("old", "unknown", "blocked_checksum"),
            ("new", "new", "match"),
        ):
            result = refork.compare_ledgers({"same.sql": old}, {"same.sql": new}, rules)
            self.assertEqual(result[0]["status"], status)
        result = refork.compare_ledgers({"other.sql": "old"}, {"other.sql": "new"}, rules)
        self.assertEqual(result[0]["status"], "blocked_checksum")

    def test_official_rules_are_read_without_implied_approval(self):
        files = refork.official_ledger()
        rules = refork.compatibility_rules()
        self.assertGreater(len(files), 200)
        self.assertGreater(len(rules), 10)
        for name, values in rules.items():
            self.assertIn(name, files)
            self.assertTrue(all(len(value) == 64 for value in values))
        # A stale upstream allowlist is not permission to accept another digest.
        result = refork.compare_ledgers(
            {"109_auth_identity_compat_backfill.sql": "551e498aa5616d2d91096e9d72cf9fb36e418ee22eacc557f8811cadbc9e20ee"},
            files, rules)
        self.assertEqual(next(r["status"] for r in result if r["filename"].startswith("109_")),
                         "blocked_checksum")

    def test_model_scope_fails_closed(self):
        for value in ({}, {"enabled": False, "models": ["glm-5"]},
                      {"enabled": True, "models": []},
                      {"enabled": True, "models": ["qwen*"]}):
            with self.assertRaises(refork.Blocked):
                refork.exact_models(value)
        self.assertEqual(refork.exact_models({"enabled": True, "models": [" model ", "MODEL"]}),
                         {"enabled": True, "models": ["model"]})

    def test_custom_compatibility_is_directional_and_pinned(self):
        exact = refork.exact_compatibility_rules()
        files = refork.official_ledger()
        self.assertEqual(len(exact), 6)
        for name, (old, new) in exact.items():
            self.assertEqual(files[name], new)
            self.assertEqual(refork.compare_ledgers({name: old}, {name: new})[0]["status"], "compatible")
            self.assertEqual(refork.compare_ledgers({name: new}, {name: old})[0]["status"], "blocked_checksum")

    def test_new_columns_do_not_hide_historical_rewrites(self):
        before = [{"id": 1, "amount": "0.10"}]
        self.assertTrue(refork.projected_rows_match(before, [{"id": 1, "amount": "0.10", "bonus": 0}]))
        self.assertFalse(refork.projected_rows_match(before, [{"id": 1, "amount": "0.11", "bonus": 0}]))
        self.assertFalse(refork.projected_rows_match(before, []))
        self.assertFalse(refork.projected_rows_match([], [{"id": 2}]))
        self.assertFalse(refork.projected_rows_match([{"id": 1, "price": None}], [{"id": 1}]))

    def test_group_column_rename_preserves_config_and_detects_value_changes(self):
        config = {"enabled": True, "models": ["explicit-model"]}
        old = {"id": 1, "models_list_config": config}
        actual = {"id": 1, "model_allowlist": config}
        self.assertEqual(refork.expected_group_fields(old, actual), actual)
        changed = {"id": 1, "model_allowlist": {}}
        self.assertNotEqual(refork.expected_group_fields(old, changed), changed)
        both = dict(actual, models_list_config=config)
        self.assertEqual(refork.expected_group_fields(old, both), old)

    def test_cache_invalidation_can_only_append_known_keys_and_preserve_old_rows(self):
        known = refork.hashlib.sha256(b"synthetic-key").hexdigest()
        before = [{"id": 1, "cache_key": known, "attempts": 2}]
        appended = before + [{"id": 2, "cache_key": known, "attempts": 0}]
        self.assertTrue(refork.invalidation_rows_match(before, appended, [{"key": "synthetic-key"}]))
        self.assertFalse(refork.invalidation_rows_match(before, appended, []))
        self.assertFalse(refork.invalidation_rows_match(before, [], [{"key": "synthetic-key"}]))
        self.assertFalse(refork.invalidation_rows_match(before, [
            {"id": 1, "cache_key": known, "attempts": 3}], [{"key": "synthetic-key"}]))

    def test_openai_conversion_only_accepts_precise_trigger_default(self):
        original = {"platform": "qwen", "extra": {"upstream_billing_manual_rate": 0}}
        self.assertEqual(refork.expected_account_extra(original), {
            "upstream_billing_manual_rate": 0, "openai_long_context_billing_enabled": False})
        original["extra"]["openai_long_context_billing_enabled"] = True
        self.assertTrue(refork.expected_account_extra(original)["openai_long_context_billing_enabled"])
        self.assertNotIn("openai_long_context_billing_enabled",
                         refork.expected_account_extra({"platform": "glm", "extra": {}}))

    def test_archive_integrity_and_exclusive_output(self):
        data = {"schema_migrations": []}
        archive = {"baseline": refork.BASELINE, "data": data, "plan": {"groups": {}}}
        archive["sha256"] = refork.archive_digest(archive)
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "archive.json"
            refork.write_private(path, archive)
            self.assertEqual(refork.load_archive(path), archive)
            with self.assertRaises(FileExistsError):
                refork.write_private(path, archive)
            archive["data"]["schema_migrations"].append("tampered")
            tampered = Path(folder) / "tampered.json"
            refork.write_private(tampered, archive)
            with self.assertRaises(refork.Blocked):
                refork.load_archive(tampered)

    def test_archive_plan_and_database_identity_are_covered_by_digest(self):
        original = {"baseline": refork.BASELINE, "database": "candidate", "plan": {"groups": {}}, "data": {}}
        digest = refork.archive_digest(original)
        for key in ("database", "plan", "baseline"):
            changed = dict(original)
            changed[key] = "tampered"
            self.assertNotEqual(refork.archive_digest(changed), digest)

    def test_numeric_evidence_does_not_round_via_binary_float(self):
        value = refork.json.loads('{"amount":123456789.123456789}', parse_float=refork.Decimal)
        self.assertIn("123456789.123456789", refork.safe_json(value))
        self.assertNotIn('"123456789.123456789"', refork.safe_json(value))

    def test_mapping_platform_conversion_preserves_model_names_and_targets(self):
        original = {"glm": {"GLM-5": "upstream-glm"}, "qwen": {"Qwen-3": "qwen3"},
                    "openai": {"gpt-example": "original"}}
        result = refork.convert_channel_mapping(original)
        self.assertEqual(result, {"zhipu": {"GLM-5": "upstream-glm"},
                                  "openai": {"Qwen-3": "qwen3", "gpt-example": "original"}})
        self.assertIn("qwen", original)
        for bad in (
            {"qwen": {"same": "a"}, "mimo": {"SAME": "b"}},
            {"qwen": {"qw*": "a"}, "openai": {"gpt": "b"}},
        ):
            with self.assertRaises(refork.Blocked):
                refork.convert_channel_mapping(bad)

    def test_pricing_conversion_refuses_cross_platform_collisions(self):
        refork.validate_pricing_platforms([
            {"channel_id": 1, "platform": "qwen", "models": ["qwen-3"]},
            {"channel_id": 1, "platform": "mimo", "models": ["mimo-2"]},
        ], "channel_id")
        for model in ("QWEN-3", "*"):
            with self.assertRaises(refork.Blocked):
                refork.validate_pricing_platforms([
                    {"channel_id": 1, "platform": "qwen", "models": ["qwen-3"]},
                    {"channel_id": 1, "platform": "mimo", "models": [model]},
                ], "channel_id")

    def test_private_evidence_cannot_be_written_to_tracked_source_paths(self):
        with self.assertRaises(refork.Blocked):
            refork.write_private(refork.ROOT / "archive-test.json", {})


if __name__ == "__main__":
    unittest.main()
