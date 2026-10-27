package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Image generation is billed per request, so its upstream cost is configured
// per model and account rather than derived from token prices. Operators need
// the models and accounts they actually use as candidates; a static catalog
// cannot supply them because upstream providers expose model IDs (for example
// gpt-image-2.5-sunburst) that appear in no shipped list.
//
// Both queries read usage history instead of configuration, so the candidates
// reflect what really ran.

// imageUsageWindowDays bounds how far back candidates are collected. Image
// models churn faster than text models, so a quarter is enough to keep the
// list useful without surfacing long-retired IDs.
const imageUsageWindowDays = 90

// ListImageGenerationModels returns the distinct image models seen in recent
// usage, most recently used first. The image predicate matches the billing
// split used by the cost reports so a model that was billed per image is also
// a model that can be priced per image.
func (r *usageLogRepository) ListImageGenerationModels(ctx context.Context) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS image_model,
		       MAX(ul.created_at) AS last_used_at
		FROM usage_logs ul
		WHERE ul.created_at >= NOW() - INTERVAL '%d days'
		  AND COALESCE(ul.image_count, 0) > 0
		  AND COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) <> ''
		GROUP BY 1
		ORDER BY last_used_at DESC
	`, imageUsageWindowDays)

	rows, err := r.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	models := make([]string, 0, 16)
	for rows.Next() {
		var model string
		var lastUsed any
		if err := rows.Scan(&model, &lastUsed); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

// ListImageGenerationAccounts returns the accounts that recently produced
// images so the operator can pick one by name instead of looking up its ID.
func (r *usageLogRepository) ListImageGenerationAccounts(ctx context.Context) ([]service.ImageUpstreamCostAccountCandidate, error) {
	query := fmt.Sprintf(`
		SELECT ul.account_id, COALESCE(a.name, '') AS account_name,
		       MAX(ul.created_at) AS last_used_at
		FROM usage_logs ul
		LEFT JOIN accounts a ON a.id = ul.account_id
		WHERE ul.created_at >= NOW() - INTERVAL '%d days'
		  AND COALESCE(ul.image_count, 0) > 0
		  AND ul.account_id > 0
		GROUP BY ul.account_id, a.name
		ORDER BY last_used_at DESC
	`, imageUsageWindowDays)

	rows, err := r.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]service.ImageUpstreamCostAccountCandidate, 0, 8)
	for rows.Next() {
		candidate := service.ImageUpstreamCostAccountCandidate{}
		var lastUsed any
		if err := rows.Scan(&candidate.AccountID, &candidate.AccountName, &lastUsed); err != nil {
			return nil, err
		}
		accounts = append(accounts, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}
