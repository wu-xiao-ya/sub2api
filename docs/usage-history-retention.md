# Preserve Usage History

Usage detail rows are distinct from hourly usage summaries. The automatic
usage archive worker aggregates old rows and then deletes the originals.
Summaries cannot reconstruct request IDs, exact timestamps, latency, or other
per-request details. User history queries do not currently read these summaries.

Two independent jobs can remove usage details:

- `usage_cleanup` can execute manual cleanup tasks and the automatic hourly
  archive worker.
- Dashboard aggregation has a separate retention cleanup for usage details,
  deduplication rows, and dashboard aggregates.

To preserve existing history while keeping dashboard statistics refreshed, use:

```text
USAGE_CLEANUP_ENABLED=false
USAGE_CLEANUP_ARCHIVE_ENABLED=false
DASHBOARD_AGGREGATION_ENABLED=true
DASHBOARD_AGGREGATION_RETENTION_ENABLED=false
```

Restart or roll the application after changing these deployment settings.
The new dashboard retention flag defaults to true for compatibility. The
deployment must explicitly set it to false while history preservation is needed.
Disabling retention does not disable aggregation or alter token billing.

Before recovery, take and verify a current database backup. Restore older dumps
only into a separate database first. Validate source IDs, foreign keys, duplicate
request IDs, and backup time coverage before importing individual records.
Do not create fabricated detail rows from hourly summaries. Do not overwrite
current balances, API keys, accounts, orders, or billing deduplication records.

When rolling back to a binary without the separate retention flag, also set
`DASHBOARD_AGGREGATION_ENABLED=false` temporarily; that binary ignores the new
flag. Restore normal aggregation only with a binary that supports the separate
retention switch or after an explicitly approved retention policy.
