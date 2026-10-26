#!/usr/bin/env python3
"""Private, candidate-only custom-format backup and independent restore proof."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

import refork


def file_digest(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def primary_key_columns(conn, table):
    from psycopg import sql

    name = sql.Identifier("public", table).as_string(conn)
    return [row[0] for row in conn.execute("""
        SELECT a.attname FROM pg_index i
        CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum, position)
        JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum
        WHERE i.indrelid=%s::regclass AND i.indisprimary ORDER BY k.position
    """, (name,))]


def fingerprint(conn):
    from psycopg import sql

    tables = [row[0] for row in conn.execute("""
        SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename
    """)]
    result = {"tables": {}, "sequences": []}
    for index, table in enumerate(tables):
        digest = hashlib.sha256()
        count = 0
        order = primary_key_columns(conn, table)
        sort = (sql.SQL(",").join(map(sql.Identifier, order)) if order
                else sql.SQL('to_jsonb(t)::text COLLATE "C"'))
        with conn.cursor(name=f"refork_backup_{index}") as cursor:
            cursor.execute(sql.SQL('SELECT to_jsonb(t)::text FROM public.{} t ORDER BY {}').format(
                sql.Identifier(table), sort))
            for (row,) in cursor:
                digest.update(row.encode("utf-8") + b"\n")
                count += 1
        result["tables"][table] = {"count": count, "sha256": digest.hexdigest(), "order_by": order}
    result["sequences"] = [list(row) for row in conn.execute("""
        SELECT sequencename, start_value, min_value, max_value, increment_by, cycle, last_value
        FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename
    """)]
    return result


def client_command(program, args):
    from psycopg.conninfo import conninfo_to_dict

    names = {"host": "PGHOST", "hostaddr": "PGHOSTADDR", "port": "PGPORT",
             "user": "PGUSER", "password": "PGPASSWORD", "dbname": "PGDATABASE",
             "sslmode": "PGSSLMODE", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY",
             "sslrootcert": "PGSSLROOTCERT", "connect_timeout": "PGCONNECT_TIMEOUT",
             "options": "PGOPTIONS", "application_name": "PGAPPNAME"}
    values = conninfo_to_dict(os.environ["DATABASE_URL"])
    if set(values) - names.keys() or not values.get("dbname"):
        raise refork.Blocked("backup client requires an explicit supported PostgreSQL connection")
    env = {key: value for key, value in os.environ.items() if not key.startswith("PG")}
    env.update({names[key]: value for key, value in values.items()})
    command = [program]
    # CI uses only synthetic data and the service container's matching PG tools.
    container = os.environ.get("REFORK_PG_CONTAINER")
    if container:
        if not re.fullmatch(r"[a-f0-9]{12,64}", container):
            raise refork.Blocked("invalid PostgreSQL client container identity")
        command = ["docker", "exec", "-i"]
        for key in names.values():
            if key in env:
                command += ["--env", key]
        command += [container, program]
    return command + args, env


def pg_command(program, args, output=None):
    command, env = client_command(program, args)
    result = subprocess.run(command, env=env, stdout=output or subprocess.PIPE,
                            stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise refork.Blocked(f"{program} failed; private backup/restore is incomplete")


def load_manifest(path):
    manifest = refork.load_archive(path)
    dump = Path(manifest["dump"]).resolve()
    if not dump.is_file() or file_digest(dump) != manifest["dump_sha256"]:
        raise refork.Blocked("backup dump missing or changed")
    return manifest


def do_create(args):
    from psycopg import sql

    dump = Path(args.dump).resolve()
    private = (refork.ROOT / "ops/refork/private").resolve()
    if refork.ROOT in dump.parents and private not in dump.parents:
        raise refork.Blocked("backup must remain outside tracked source")
    with refork.connect() as conn:
        conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
        refork.candidate_guard(conn, args.candidate_database)
        tables = [row[0] for row in conn.execute("""
            SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename
        """)]
        if not tables:
            raise refork.Blocked("cannot back up an empty application database")
        conn.execute(sql.SQL("LOCK TABLE {} IN SHARE MODE").format(
            sql.SQL(",").join(sql.Identifier("public", table) for table in tables)))
        exported = conn.execute("SELECT pg_export_snapshot()").fetchone()[0]
        dump.parent.mkdir(parents=True, exist_ok=True)
        fd = os.open(dump, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as stream:
            pg_command("pg_dump", ["--format=custom", "--no-owner", "--no-privileges",
                                   "--snapshot", exported], stream)
        manifest = {"baseline": refork.BASELINE, "database": args.candidate_database,
                    "dump": str(dump), "dump_sha256": file_digest(dump), "data": fingerprint(conn)}
        manifest["sha256"] = refork.archive_digest(manifest)
        refork.write_private(args.manifest, manifest)
    print("private full backup complete; independent restore verification still required")


def do_verify(args):
    manifest = load_manifest(args.manifest)
    if manifest["database"] == args.candidate_database:
        raise refork.Blocked("restore verification requires a different independent candidate database")
    with refork.connect() as conn:
        refork.candidate_guard(conn, args.candidate_database)
        count = conn.execute("""SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
            WHERE n.nspname NOT IN ('pg_catalog','information_schema')
              AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','f')""").fetchone()[0]
        if count:
            raise refork.Blocked("restore destination must contain no application objects")
    # Stream the custom dump through stdin: host paths and DSNs never enter SQL.
    command, env = client_command("pg_restore", ["--dbname", args.candidate_database, "--no-owner",
                                               "--no-privileges", "--exit-on-error", "--single-transaction"])
    with Path(manifest["dump"]).open("rb") as stream:
        result = subprocess.run(command, env=env, stdin=stream, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise refork.Blocked("pg_restore failed; discard this partial candidate restore")
    with refork.connect(read_only=True) as conn:
        actual = fingerprint(conn)
    if actual != manifest["data"]:
        raise refork.Blocked("restored table data or sequence values differ from the backup")
    proof = {"baseline": refork.BASELINE, "database": manifest["database"],
             "restored_database": args.candidate_database, "dump": manifest["dump"],
             "dump_sha256": manifest["dump_sha256"], "manifest_sha256": manifest["sha256"],
             "data": actual, "restore_verified": True}
    proof["sha256"] = refork.archive_digest(proof)
    refork.write_private(args.proof, proof)
    print("independent full restore verified; private proof created")


def validate_proof(path, conn, database):
    proof = load_manifest(path)
    if proof.get("restore_verified") is not True or proof.get("database") != database:
        raise refork.Blocked("verified full backup proof does not match this candidate")
    if proof.get("restored_database") == database or not proof.get("restored_database"):
        raise refork.Blocked("independent restore identity missing")
    if fingerprint(conn) != proof["data"]:
        raise refork.Blocked("candidate drifted after verified full backup")
    return {key: proof[key] for key in
            ("dump_sha256", "manifest_sha256", "restored_database", "sha256")}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    create = sub.add_parser("create")
    create.add_argument("--candidate-database", required=True)
    create.add_argument("--dump", required=True)
    create.add_argument("--manifest", required=True)
    verify = sub.add_parser("verify")
    verify.add_argument("--candidate-database", required=True)
    verify.add_argument("--manifest", required=True)
    verify.add_argument("--proof", required=True)
    args = parser.parse_args()
    try:
        globals()["do_" + args.command](args)
    except refork.Blocked as exc:
        print(f"BLOCKED: {exc}", file=sys.stderr)
        return 2
    except Exception:
        print("FAILED: private backup/restore aborted", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
