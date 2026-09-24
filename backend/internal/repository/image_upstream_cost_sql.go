package repository

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Image upstream cost reporting shares one SQL definition across every report
// path. Keeping the expression in a single builder prevents the dashboard,
// stats and trend endpoints from disagreeing about the same usage rows, which
// previously happened because only the trend query knew about per-image cost.
//
// Priority, highest first:
//  1. account_model (account + model + tier)
//  2. model (model + tier)
//  3. account (flat per-account value, any model/tier)
//  4. global default
//
// Tier resolution uses the usage row's image_size, normalized to 1K/2K/4K. A
// row whose size is unknown bills at 2K, matching the revenue-side
// NormalizeImageBillingTierOrDefault default.

// imageUpstreamCostCTE declares the runtime settings used by the image cost
// expression. Every query that references imageAccountCostExpr must include it.
//
// The settings are read as text and cast defensively: a malformed operator
// value must degrade to the next fallback rather than fail the report.
func imageUpstreamCostCTE() string {
	return `
		runtime_settings AS (
			SELECT COALESCE(
				(
					SELECT CASE
						WHEN TRIM(value) ~ '^[0-9]+(\.[0-9]+)?$'
						THEN TRIM(value)::double precision
						ELSE NULL
					END
					FROM settings
					WHERE key = 'image_upstream_cost_per_image'
					LIMIT 1
				),
				0.001::double precision
			) AS image_upstream_cost_per_image,
			COALESCE(
				(
					SELECT CASE
						WHEN jsonb_typeof(value::jsonb) = 'object' THEN value::jsonb
						ELSE '{}'::jsonb
					END
					FROM settings
					WHERE key = 'image_upstream_cost_by_account'
					LIMIT 1
				),
				'{}'::jsonb
			) AS image_upstream_cost_by_account,
			COALESCE(
				(
					SELECT CASE
						WHEN jsonb_typeof(value::jsonb) = 'object' THEN value::jsonb
						ELSE '{}'::jsonb
					END
					FROM settings
					WHERE key = 'image_upstream_cost_by_model'
					LIMIT 1
				),
				'{}'::jsonb
			) AS image_upstream_cost_by_model,
			COALESCE(
				(
					SELECT CASE
						WHEN jsonb_typeof(value::jsonb) = 'object' THEN value::jsonb
						ELSE '{}'::jsonb
					END
					FROM settings
					WHERE key = 'image_upstream_cost_by_account_model'
					LIMIT 1
				),
				'{}'::jsonb
			) AS image_upstream_cost_by_account_model,
			COALESCE(
				(
					SELECT LOWER(TRIM(value)) = 'true'
					FROM settings
					WHERE key = 'image_cost_ignore_upstream_rate_snapshot'
					LIMIT 1
				),
				false
			) AS image_cost_ignore_upstream_rate_snapshot
		)`
}

// imageSettingValueExpr reads one image-cost setting as an inline scalar
// subquery. Queries that cannot host the runtime_settings CTE (small dashboard
// sub-selects) use this variant so they still share the same cost semantics.
func imageSettingValueExpr(key string, jsonObject bool) string {
	if jsonObject {
		return fmt.Sprintf(`COALESCE(
			(SELECT CASE
				WHEN jsonb_typeof(value::jsonb) = 'object' THEN value::jsonb
				ELSE '{}'::jsonb
			END FROM settings WHERE key = '%s' LIMIT 1),
			'{}'::jsonb
		)`, key)
	}
	return fmt.Sprintf(`COALESCE(
		(SELECT CASE
			WHEN TRIM(value) ~ '^[0-9]+(\.[0-9]+)?$' THEN TRIM(value)::double precision
			ELSE NULL
		END FROM settings WHERE key = '%s' LIMIT 1),
		%s::double precision
	)`, key, "0.001")
}

// imageIgnoreRateSnapshotExpr reads the snapshot-ignore switch inline.
func imageIgnoreRateSnapshotExpr() string {
	return fmt.Sprintf(`COALESCE(
		(SELECT LOWER(TRIM(value)) = 'true' FROM settings WHERE key = '%s' LIMIT 1),
		false
	)`, service.SettingKeyImageCostIgnoreUpstreamRateSnapshot)
}

func imageBillingTierExpr(alias string) string {
	size := fmt.Sprintf("LOWER(TRIM(COALESCE(%s.image_size, '')))", alias)
	return fmt.Sprintf(`(CASE
		WHEN %[1]s IN ('1k', '1024x1024') THEN '1K'
		WHEN %[1]s IN ('2k', '2048x2048', '2048x1152') THEN '2K'
		WHEN %[1]s IN ('4k', '3840x2160', '2160x3840') THEN '4K'
		ELSE '2K'
	END)`, size)
}

// imageRowIsImageBilledExpr reports whether a usage row is billed per image
// rather than per token.
func imageRowIsImageBilledExpr(alias string) string {
	return fmt.Sprintf(`(COALESCE(%[1]s.billing_mode, '') = 'image'
		OR (COALESCE(%[1]s.billing_mode, '') = '' AND COALESCE(%[1]s.image_count, 0) > 0))`, alias)
}

// numericJsonExpr extracts a non-negative numeric value from a nested jsonb
// lookup chain, returning SQL NULL when any key is missing or the value is not
// a plain non-negative number. Keys are passed as SQL expressions so callers
// can build per-row (account, model) lookups.
//
// The chained -> operator yields jsonb, which has no regex operator, so the
// result is cast to text before validation.
func numericJsonExpr(root string, keys ...string) string {
	expr := root
	for _, key := range keys {
		expr = fmt.Sprintf("(%s -> %s)", expr, key)
	}
	text := fmt.Sprintf("(%s)::text", expr)
	return fmt.Sprintf(`CASE
		WHEN %[1]s ~ '^[0-9]+(\.[0-9]+)?$'
		THEN %[1]s::double precision
	END`, text)
}

// imageUnitCostExprSource resolves the upstream cost of a single generated
// image, applying the full priority chain. The src argument supplies the four
// setting expressions in priority order (account+model, model, account,
// default); rs-based and inline callers differ only there.
func imageUnitCostExprSource(alias string, accountModel, model, account, def string) string {
	tier := imageBillingTierExpr(alias)
	accountKey := fmt.Sprintf("%s.account_id::text", alias)
	modelKey := fmt.Sprintf("LOWER(TRIM(COALESCE(%s.requested_model, %s.model, '')))", alias, alias)

	return fmt.Sprintf(`COALESCE(
		%s,
		%s,
		%s,
		%s
	)`,
		numericJsonExpr(accountModel, accountKey, modelKey, tier),
		numericJsonExpr(model, modelKey, tier),
		numericJsonExpr(account, accountKey),
		def,
	)
}

// imageUnitCostExpr resolves the upstream cost of a single generated image in a
// query that declares imageUpstreamCostCTE().
func imageUnitCostExpr(alias string) string {
	return imageUnitCostExprSource(alias,
		"rs.image_upstream_cost_by_account_model",
		"rs.image_upstream_cost_by_model",
		"rs.image_upstream_cost_by_account",
		"rs.image_upstream_cost_per_image",
	)
}

// inlineImageUnitCostExpr is the self-contained variant for queries that cannot
// declare the runtime_settings CTE. Requires a usage_logs row aliased alias.
func inlineImageUnitCostExpr(alias string) string {
	return imageUnitCostExprSource(alias,
		imageSettingValueExpr(service.SettingKeyImageUpstreamCostByAccountModel, true),
		imageSettingValueExpr(service.SettingKeyImageUpstreamCostByModel, true),
		imageSettingValueExpr(service.SettingKeyImageUpstreamCostByAccount, true),
		imageSettingValueExpr(service.SettingKeyImageUpstreamCostPerImage, false),
	)
}

// inlineImageAwareAccountCostExpr is the CTE-free counterpart of
// imageAwareAccountCostExpr for short dashboard sub-selects.
func inlineImageAwareAccountCostExpr(alias string) string {
	return fmt.Sprintf(`(CASE
		WHEN %[1]s AND %[3]s
		THEN GREATEST(COALESCE(%[2]s.image_count, 0), 0) * (%[4]s)
		ELSE COALESCE(%[2]s.account_stats_cost, %[2]s.total_cost) * COALESCE(%[2]s.account_rate_multiplier, 1)
	END)`,
		imageRowIsImageBilledExpr(alias),
		alias,
		imageIgnoreRateSnapshotExpr(),
		inlineImageUnitCostExpr(alias),
	)
}

// imageAccountCostExpr is the upstream cost of one usage row before the
// account_rate_multiplier is applied: image rows bill per generated image,
// every other row keeps the existing token accounting.
func imageAccountCostExpr(alias string) string {
	return fmt.Sprintf(`(CASE
		WHEN %[1]s
		THEN GREATEST(COALESCE(%[2]s.image_count, 0), 0) * (%[3]s)
		ELSE COALESCE(%[2]s.account_stats_cost, %[2]s.total_cost)
	END)`,
		imageRowIsImageBilledExpr(alias),
		alias,
		imageUnitCostExpr(alias),
	)
}

// imageAwareAccountCostExpr is the single source of truth for the account-side
// cost of any usage row, including the rate multiplier.
//
// Image rows only switch to per-image accounting when the operator turns on
// image_cost_ignore_upstream_rate_snapshot after configuring a real per-image
// price. Until then they keep the historical token accounting
// (account_stats_cost × account_rate_multiplier), so enabling this feature is
// the only thing that moves existing report numbers.
//
// Requires imageUpstreamCostCTE() in the enclosing query.
func imageAwareAccountCostExpr(alias string) string {
	return fmt.Sprintf(`(CASE
		WHEN %[1]s AND %[3]s
		THEN GREATEST(COALESCE(%[2]s.image_count, 0), 0) * (%[4]s)
		ELSE COALESCE(%[2]s.account_stats_cost, %[2]s.total_cost) * COALESCE(%[2]s.account_rate_multiplier, 1)
	END)`,
		imageRowIsImageBilledExpr(alias),
		alias,
		imageIgnoreRateSnapshotExprCTE(),
		imageUnitCostExpr(alias),
	)
}

// imageAccountCostExprCTE is the pre-multiplier counterpart of
// imageAwareAccountCostExpr for queries that separate standard and legacy cost.
func imageAccountCostExprCTE(alias string) string {
	return fmt.Sprintf(`(CASE
		WHEN %[1]s AND %[3]s
		THEN GREATEST(COALESCE(%[2]s.image_count, 0), 0) * (%[4]s)
		ELSE COALESCE(%[2]s.account_stats_cost, %[2]s.total_cost)
	END)`,
		imageRowIsImageBilledExpr(alias),
		alias,
		imageIgnoreRateSnapshotExprCTE(),
		imageUnitCostExpr(alias),
	)
}

// imageIgnoreRateSnapshotExprCTE reads the switch from the runtime_settings CTE.
func imageIgnoreRateSnapshotExprCTE() string {
	return "rs.image_cost_ignore_upstream_rate_snapshot"
}
