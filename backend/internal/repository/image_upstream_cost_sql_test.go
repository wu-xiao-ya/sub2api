package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The shared cost expressions are string-built SQL, so these tests pin the
// structural properties that would silently corrupt every report path:
// required settings keys, alias usage and the image/token split.
func TestImageUpstreamCostCTE_DeclaresEveryKey(t *testing.T) {
	cte := imageUpstreamCostCTE()
	for _, key := range []string{
		"image_upstream_cost_per_image",
		"image_upstream_cost_by_account",
		"image_upstream_cost_by_model",
		"image_upstream_cost_by_account_model",
		"image_cost_ignore_upstream_rate_snapshot",
	} {
		require.Contains(t, cte, key, "CTE must read %s", key)
	}
	require.Contains(t, cte, "runtime_settings", "CTE must keep its stable name")
}

func TestImageAwareAccountCostExpr_KeepsTokenBranch(t *testing.T) {
	expr := imageAwareAccountCostExpr("ul")

	// Image rows bill per image, everything else keeps the token accounting.
	require.Contains(t, expr, "GREATEST(COALESCE(ul.image_count, 0), 0)")
	require.Contains(t, expr, "COALESCE(ul.account_stats_cost, ul.total_cost)")
	require.Contains(t, expr, "ul.account_rate_multiplier")
	// The switch must gate the per-image branch, so the default stays compatible.
	require.Contains(t, expr, "rs.image_cost_ignore_upstream_rate_snapshot")
}

func TestImageAccountCostExprCTE_SeparatesStandardAndLegacy(t *testing.T) {
	expr := imageAccountCostExprCTE("ul")

	// The pre-multiplier variant must not apply account_rate_multiplier itself;
	// trend queries add it once for the legacy column.
	require.NotContains(t, expr, "ul.account_rate_multiplier")
	require.Contains(t, expr, "COALESCE(ul.account_stats_cost, ul.total_cost)")
	require.Contains(t, expr, "rs.image_cost_ignore_upstream_rate_snapshot")
}

func TestInlineImageAwareAccountCostExpr_IsSelfContained(t *testing.T) {
	expr := inlineImageAwareAccountCostExpr("ul")

	// No CTE reference may leak into the inline variant, or every dashboard query
	// using it would fail with a missing FROM-clause entry.
	require.NotContains(t, expr, "rs.")
	require.Contains(t, expr, "FROM settings")
	require.Contains(t, expr, "COALESCE(ul.account_stats_cost, ul.total_cost)")
}

func TestImageBillingTierExpr_NormalizesKnownSizes(t *testing.T) {
	expr := imageBillingTierExpr("ul")
	for _, token := range []string{"'1k'", "'1024x1024'", "'2k'", "'2048x2048'", "'4k'", "'3840x2160'"} {
		require.Contains(t, expr, token)
	}
	// Unknown sizes must land on 2K, matching NormalizeImageBillingTierOrDefault.
	require.Contains(t, expr, "ELSE '2K'")
}

func TestNumericJsonExpr_CastsBeforeRegex(t *testing.T) {
	expr := numericJsonExpr("rs.some_json", "ul.account_id::text", "'2K'")

	// jsonb has no regex operator, so the chained lookup must be cast to text.
	require.Contains(t, expr, "::text")
	require.Contains(t, expr, "~ '^[0-9]+(\\.[0-9]+)?$'")
	require.Contains(t, expr, "::double precision")
}

func TestImageRowIsImageBilledExpr_MatchesBillingModes(t *testing.T) {
	expr := imageRowIsImageBilledExpr("ul")
	require.Contains(t, expr, "ul.billing_mode, '') = 'image'")
	// Legacy rows without billing_mode but with an image count still count.
	require.Contains(t, expr, "ul.image_count, 0) > 0")
}

// Both variants must resolve the same four priority levels in the same order.
func TestImageUnitCostExpr_PriorityLevels(t *testing.T) {
	// The CTE variant references columns; the inline variant inlines settings
	// lookups. Key names nest ("..._by_account" is a prefix of
	// "..._by_account_model"), so match on the full identifier boundary.
	cases := map[string]struct {
		expr    string
		markers []string
	}{
		"cte": {
			expr: imageUnitCostExpr("ul"),
			markers: []string{
				"(rs.image_upstream_cost_by_account_model -> ",
				"(rs.image_upstream_cost_by_model -> ",
				"(rs.image_upstream_cost_by_account -> ",
				"rs.image_upstream_cost_per_image",
			},
		},
		"inline": {
			expr: inlineImageUnitCostExpr("ul"),
			markers: []string{
				"'image_upstream_cost_by_account_model'",
				"'image_upstream_cost_by_model'",
				"'image_upstream_cost_by_account'",
				"'image_upstream_cost_per_image'",
			},
		},
	}

	for name, tc := range cases {
		lastIndex := -1
		for _, marker := range tc.markers {
			idx := strings.Index(tc.expr, marker)
			require.Greaterf(t, idx, lastIndex, "%s: %s must appear in priority order", name, marker)
			lastIndex = idx
		}
	}
}
