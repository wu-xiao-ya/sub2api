[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ConnectionString,

    [string]$PsqlPath = "psql",

    [string]$RepositoryRoot = (Join-Path $PSScriptRoot "..")
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ConnectionString)) {
    throw "ConnectionString is required. This script never discovers or selects a production database automatically."
}

$repo = (Resolve-Path -LiteralPath $RepositoryRoot).Path
$migrationRoot = Join-Path $repo "backend\migrations"
$migrationNames = @(
    "231_subscription_purchases_and_multi_group_plans.sql",
    "232_usage_log_subscription_purchase_attribution.sql",
    "233_backfill_and_freeze_native_subscriptions.sql",
    "234_backfill_legacy_usage_purchase_attribution.sql"
)

foreach ($name in $migrationNames) {
    $path = Join-Path $migrationRoot $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing retirement migration: $path"
    }
}

$forbiddenRuntimePatterns = @(
    "UPDATE\s+user_subscriptions",
    "INSERT\s+INTO\s+user_subscriptions",
    "DELETE\s+FROM\s+user_subscriptions",
    "UserSubscription\.(Create|Update|Delete)"
)
$runtimeFiles = Get-ChildItem -LiteralPath (Join-Path $repo "backend\internal") -Recurse -File -Filter "*.go" |
    Where-Object { $_.FullName -notmatch "_test\.go$" }
foreach ($pattern in $forbiddenRuntimePatterns) {
    $matches = Select-String -Path $runtimeFiles.FullName -Pattern $pattern -CaseSensitive
    if ($matches) {
        $details = ($matches | ForEach-Object { "$($_.Path):$($_.LineNumber): $($_.Line.Trim())" }) -join "`n"
        throw "Retired native subscription write path detected:`n$details"
    }
}

$sql = @"
BEGIN READ ONLY;
SELECT 'table_exists' AS check_name,
       CASE WHEN to_regclass('public.user_subscriptions') IS NOT NULL THEN 'yes' ELSE 'no' END AS value;
SELECT 'readonly_trigger' AS check_name,
       CASE WHEN EXISTS (
           SELECT 1
           FROM pg_trigger
           WHERE tgname = 'trg_user_subscriptions_read_only'
             AND tgrelid = to_regclass('public.user_subscriptions')
             AND NOT tgisinternal
             AND tgenabled IN ('O', 'R', 'A')
       ) THEN 'enabled' ELSE 'missing_or_disabled' END AS value;
SELECT 'unattributed_legacy_usage' AS check_name,
       COUNT(*)::text AS value
FROM usage_logs
WHERE subscription_purchase_id IS NULL
  AND subscription_id IS NOT NULL
  AND subscription_id > 0;
SELECT 'active_native_rows' AS check_name,
       COUNT(*)::text AS value
FROM user_subscriptions
WHERE deleted_at IS NULL
  AND status = 'active'
  AND expires_at > NOW();
SELECT 'legacy_purchase_rows' AS check_name,
       COUNT(*)::text AS value
FROM subscription_purchases
WHERE source = 'legacy_user_subscription';
SELECT 'purchase_group_snapshot_mismatches' AS check_name,
       COUNT(*)::text AS value
FROM subscription_purchases p
LEFT JOIN LATERAL (
    SELECT COUNT(*)::bigint AS group_count
    FROM subscription_purchase_groups pg
    WHERE pg.purchase_id = p.id
) snapshot ON TRUE
WHERE p.source = 'legacy_user_subscription'
  AND snapshot.group_count <> 1;
ROLLBACK;
"@

& $PsqlPath $ConnectionString --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --expanded --command $sql
if ($LASTEXITCODE -ne 0) {
    throw "psql exited with code $LASTEXITCODE"
}
