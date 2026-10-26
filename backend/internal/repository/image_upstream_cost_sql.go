package repository

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func imageCostJSONSetting(key string) string {
	return fmt.Sprintf(`COALESCE((SELECT CASE
		WHEN pg_input_is_valid(value, 'jsonb') THEN
			CASE WHEN jsonb_typeof(value::jsonb) = 'object' THEN value::jsonb ELSE '{}'::jsonb END
		ELSE '{}'::jsonb END FROM settings WHERE key = '%s' LIMIT 1), '{}'::jsonb)`, key)
}

func imageCostNumber(expr string) string {
	return fmt.Sprintf(`(CASE WHEN jsonb_typeof(%[1]s) = 'number' THEN
		CASE WHEN (%[1]s)::numeric BETWEEN 0 AND 1000000
		THEN (%[1]s)::double precision END END)`, expr)
}

func imageCostModelLookup(root, model string) string {
	// Match old persisted mixed-case keys as well as newly canonicalized writes.
	return fmt.Sprintf(`(SELECT value FROM jsonb_each(CASE WHEN jsonb_typeof(%[1]s) = 'object'
		THEN %[1]s ELSE '{}'::jsonb END) WHERE lower(btrim(key)) = %[2]s ORDER BY key LIMIT 1)`, root, model)
}

func inlineImageAwareAccountCostExpr(alias string) string {
	model := fmt.Sprintf("lower(btrim(COALESCE(NULLIF(%s.requested_model, ''), %s.model, '')))", alias, alias)
	tier := fmt.Sprintf(`(CASE lower(btrim(COALESCE(%[1]s.image_size,'')))
		WHEN '1k' THEN '1K' WHEN '1024x1024' THEN '1K'
		WHEN '4k' THEN '4K' WHEN '3840x2160' THEN '4K' WHEN '2160x3840' THEN '4K'
		ELSE COALESCE((SELECT CASE WHEN (dims[1])::numeric > 0 AND (dims[2])::numeric > 0
			AND GREATEST((dims[1])::numeric,(dims[2])::numeric) <= 9223372036854775807
			THEN CASE WHEN GREATEST((dims[1])::numeric,(dims[2])::numeric) <= 1024 THEN '1K'
			WHEN GREATEST((dims[1])::numeric,(dims[2])::numeric) <= 2048 THEN '2K' ELSE '4K' END END
			FROM (SELECT regexp_match(lower(btrim(COALESCE(%[1]s.image_size,''))),
				'^([+]?[0-9]+)[[:space:]]*x[[:space:]]*([+]?[0-9]+)$') AS dims) parsed), '2K') END)`, alias)
	account := alias + ".account_id::text"
	accountModel := "(" + imageCostJSONSetting(service.SettingKeyImageUpstreamCostByAccountModel) + " -> " + account + ")"
	byModel := imageCostJSONSetting(service.SettingKeyImageUpstreamCostByModel)
	byAccount := imageCostJSONSetting(service.SettingKeyImageUpstreamCostByAccount)
	unit := fmt.Sprintf(`COALESCE(%s, %s, %s,
		(SELECT CASE WHEN pg_input_is_valid(value, 'numeric') THEN
			CASE WHEN value::numeric BETWEEN 0 AND 1000000 THEN value::double precision END
			END FROM settings WHERE key = '%s' LIMIT 1), 0.001)`,
		imageCostNumber("("+imageCostModelLookup(accountModel, model)+" -> "+tier+")"),
		imageCostNumber("("+imageCostModelLookup(byModel, model)+" -> "+tier+")"),
		imageCostNumber("("+byAccount+" -> "+account+")"),
		service.SettingKeyImageUpstreamCostPerImage)
	return fmt.Sprintf(`(CASE WHEN
		(COALESCE(%[1]s.billing_mode, '') = 'image'
		 OR (COALESCE(%[1]s.billing_mode, '') = '' AND COALESCE(%[1]s.image_count,0) > 0))
		AND COALESCE((SELECT lower(btrim(value)) = 'true' FROM settings
			WHERE key = '%[3]s' LIMIT 1), false)
		THEN GREATEST(COALESCE(%[1]s.image_count,0),0) * (%[2]s)
		ELSE COALESCE(%[1]s.account_stats_cost, %[1]s.total_cost) * COALESCE(%[1]s.account_rate_multiplier,1)
	END)`, alias, unit, service.SettingKeyImageCostIgnoreUpstreamRateSnapshot)
}
