"""Restore display brands from exact-ID refork provenance. Preview is read-only by default."""
import argparse
import json
import os
from pathlib import Path

BRANDS = {"qwen", "hunyuan", "mimo"}

def classify(group, provenance, account_platforms):
    brand = provenance.get("before_row", {}).get("platform") if provenance else None
    if brand not in BRANDS:
        return None, "missing_brand_provenance"
    after = provenance["after_row"]
    if group["platform"] != "openai" or after.get("platform") != "openai":
        return None, "transport_changed"
    if group.get("upstream_provider") == brand:
        return None, "already_applied"
    if group.get("upstream_provider"):
        return None, "explicit_brand_conflict"
    if group.get("model_allowlist") != after.get("model_allowlist"):
        return None, "model_scope_changed"
    if any(p not in (brand, None) for p in account_platforms):
        return None, "mixed_account_brands"
    if any(p is None for p in account_platforms):
        return None, "unknown_account_provenance"
    return brand, "ready"

def run(conn, apply=False):
    # Preview is one consistent, read-only snapshot. Apply re-evaluates under locks.
    conn.execute("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE" + ("" if apply else " READ ONLY"))
    groups = conn.execute("SELECT id,platform,upstream_provider,model_allowlist FROM groups WHERE platform='openai' AND deleted_at IS NULL ORDER BY id" + (" FOR UPDATE" if apply else "")).fetchall()
    exists = conn.execute("SELECT to_regclass('refork_audit.conversions')").fetchone()[0]
    provenance = {}
    if exists:
        for entity, entity_id, before, after in conn.execute("SELECT entity,entity_id,before_row,after_row FROM refork_audit.conversions WHERE entity IN ('groups','accounts') ORDER BY converted_at"):
            provenance[(entity,entity_id)] = {"before_row":before,"after_row":after}
    memberships = {}
    for gid, aid in conn.execute("SELECT ag.group_id,ag.account_id FROM account_groups ag JOIN accounts a ON a.id=ag.account_id AND a.deleted_at IS NULL ORDER BY 1,2" + (" FOR SHARE OF ag,a" if apply else "")):
        memberships.setdefault(gid,[]).append(aid)
    report = {"applied": apply, "groups": []}
    if apply:
        conn.execute("CREATE SCHEMA IF NOT EXISTS refork_audit")
        conn.execute("""CREATE TABLE IF NOT EXISTS refork_audit.group_brand_updates (
          group_id bigint PRIMARY KEY, old_provider text NOT NULL, new_provider text NOT NULL,
          evidence jsonb NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())""")
    for gid, platform, provider, allowlist in groups:
        group = dict(id=gid,platform=platform,upstream_provider=provider,model_allowlist=allowlist)
        evidence = provenance.get(("groups",gid))
        account_brands = [provenance.get(("accounts",aid),{}).get("before_row",{}).get("platform") for aid in memberships.get(gid,[])]
        brand, reason = classify(group,evidence,account_brands)
        report["groups"].append(dict(group_id=gid,proposed_provider=brand,status=reason))
        if apply and brand:
            from psycopg.types.json import Jsonb
            conn.execute("INSERT INTO refork_audit.group_brand_updates(group_id,old_provider,new_provider,evidence) VALUES (%s,%s,%s,%s)",(gid,provider,brand,Jsonb(evidence)))
            conn.execute("UPDATE groups SET upstream_provider=%s,updated_at=now() WHERE id=%s",(brand,gid))
            conn.execute("INSERT INTO scheduler_outbox(event_type,group_id) VALUES ('group_changed',%s)",(gid,))
    return report

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dsn-env',default='DATABASE_URL',help='Environment variable containing the database DSN')
    parser.add_argument('--apply',action='store_true')
    parser.add_argument('--output',required=True,help='Private JSON report path, never commit production reports')
    args=parser.parse_args()
    import psycopg
    with psycopg.connect(os.environ[args.dsn_env]) as conn:
        report=run(conn,args.apply)
    output=Path(args.output)
    output.write_text(json.dumps(report,indent=2),encoding='utf-8')
    print(json.dumps({"applied":args.apply,"ready":sum(g['status']=='ready' for g in report['groups']),"report":str(output)}))

if __name__=='__main__':main()
