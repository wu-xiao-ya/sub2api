import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import backup
import refork


class BackupSafetyTests(unittest.TestCase):
    def test_connection_credentials_never_enter_client_argv(self):
        with patch.dict(os.environ, {"DATABASE_URL":
                        "postgresql://fixture:synthetic-secret@localhost:5432/test_candidate?sslmode=disable",
                        "REFORK_PG_CONTAINER": ""}):
            command, env = backup.client_command("pg_dump", ["--format=custom"])
        self.assertEqual(command, ["pg_dump", "--format=custom"])
        self.assertEqual(env["PGPASSWORD"], "synthetic-secret")
        self.assertEqual(env["PGDATABASE"], "test_candidate")
        self.assertNotIn("synthetic-secret", repr(command))

    def test_container_clients_receive_env_names_not_values(self):
        container = "a" * 64
        with patch.dict(os.environ, {"DATABASE_URL": "postgresql://fixture:synthetic@localhost/test_candidate",
                                     "REFORK_PG_CONTAINER": container}):
            command, env = backup.client_command("pg_restore", ["--dbname", "test_candidate"])
        self.assertIn("PGPASSWORD", command)
        self.assertIn(container, command)
        self.assertNotIn("synthetic", repr(command))
        self.assertEqual(env["PGPASSWORD"], "synthetic")
        with patch.dict(os.environ, {"DATABASE_URL": "postgresql://localhost/test_candidate",
                                     "REFORK_PG_CONTAINER": "untrusted;command"}):
            with self.assertRaises(refork.Blocked):
                backup.client_command("pg_dump", [])

    def test_proof_rejects_dump_tampering_and_drift(self):
        with tempfile.TemporaryDirectory() as folder:
            dump = Path(folder) / "backup.dump"
            # Synthetic bytes only; this test does not pretend to verify a restore.
            dump.touch()
            proof = {"baseline": refork.BASELINE, "database": "source_candidate",
                     "restored_database": "restore_candidate", "dump": str(dump),
                     "dump_sha256": backup.file_digest(dump), "manifest_sha256": "1" * 64,
                     "data": {"tables": {}}, "restore_verified": True}
            proof["sha256"] = refork.archive_digest(proof)
            path = Path(folder) / "proof.json"
            refork.write_private(path, proof)
            with patch.object(backup, "fingerprint", return_value=proof["data"]):
                backup.validate_proof(path, object(), "source_candidate")
                with self.assertRaises(refork.Blocked):
                    backup.validate_proof(path, object(), "wrong_candidate")
            with patch.object(backup, "fingerprint", return_value={"changed": True}):
                with self.assertRaises(refork.Blocked):
                    backup.validate_proof(path, object(), "source_candidate")
            dump.unlink()
            with self.assertRaises(refork.Blocked):
                backup.load_manifest(path)


if __name__ == "__main__":
    unittest.main()
