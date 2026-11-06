# Domestic upstream brands

Qwen, Hunyuan and MiMo are display brands on OpenAI-compatible groups. Set `upstream_provider` to `qwen`, `hunyuan`, `mimo` or the empty string in the existing group create/update APIs. Omission on update preserves the brand; empty string clears it. Moving away from OpenAI clears an omitted brand; explicitly incompatible combinations are rejected. No protocol capabilities, endpoints, official account logins, balance probes or model IDs are inferred from a brand. Use existing account endpoint/protocol/model-mapping settings and explicit group model scopes.

## Existing groups

Apply migration257 with the normal runner. Preview with `DATABASE_URL` set privately:

```powershell
python ops/refork/group_brands.py --output <private-report-path>
```

The default transaction is read-only and identifies groups by migration conversion IDs. Missing evidence, changed model scope, explicit conflicting brands or mixed/unknown account provenance remain unchanged and appear in the report. Review the report and archive a complete database/config backup before using `--apply` on an isolated copy. Apply records exact evidence and old/new values in `refork_audit.group_brand_updates`, uses one serializable transaction, and leaves financial records, bindings and routing platforms intact. Reruns do not duplicate updates. Native auth invalidation and scheduler outboxes refresh caches. Use the admin group editor for groups requiring manual decisions; never infer brands from names.

## Delivery and production rollout

Run generated-code verification, unit/database integration tests, full frontend checks and isolated mock inference tests for all brands. Compare normal/aggregate routing and fixed-sample costs to unbranded OpenAI groups. Keep Kimi/DeepSeek/Zhipu unchanged. Produce an immutable candidate digest and private rehearsal report. Production rollout is a separate authorized step: archive database/config, drain traffic, apply normal migrations, run reviewed backfill, start the pinned image and verify public assets/inference/billing. Preserve the prior application image and current database on rollback; do not restore an old dump over later financial writes. The additive field is ignored by the preceding app; migration checksums remain enforced.
