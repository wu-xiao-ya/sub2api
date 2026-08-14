package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// ListRecentChannelMonitorTraffic returns recent successful end-user requests
// that can be safely attributed to one account-management group and requested
// monitor model. It deliberately queries only the recent raw-log window:
// archived hourly data lacks usage_source and cannot reliably exclude probes.
func (r *usageLogRepository) ListRecentChannelMonitorTraffic(
	ctx context.Context,
	groupID int64,
	accountIDs []int64,
	models []string,
	since time.Time,
	limit int,
) ([]service.ChannelMonitorTrafficSample, error) {
	if groupID <= 0 || len(accountIDs) == 0 || len(models) == 0 {
		return []service.ChannelMonitorTrafficSample{}, nil
	}
	cleanModels := make([]string, 0, len(models))
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			cleanModels = append(cleanModels, model)
		}
	}
	if len(cleanModels) == 0 {
		return []service.ChannelMonitorTrafficSample{}, nil
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	requestedModel := resolveModelDimensionExpressionWithAlias("requested", "ul")
	const base = `
		SELECT ul.account_id, %s AS model, ul.duration_ms, ul.first_token_ms, ul.created_at
		FROM usage_logs ul
		WHERE ul.account_id = ANY($2)
		  AND EXISTS (
			SELECT 1
			FROM account_groups ag
			WHERE ag.account_id = ul.account_id
			  AND ag.group_id = $1
		  )
		  AND %s = ANY($3)
		  AND ul.created_at >= $4
		  AND ul.actual_cost > 0
		  AND ul.duration_ms IS NOT NULL
		  AND ul.duration_ms > 0
		  AND (ul.usage_source IS NULL OR ul.usage_source <> 'channel_monitor')
		ORDER BY ul.created_at DESC
		LIMIT $5
	`
	query := fmt.Sprintf(base, requestedModel, requestedModel)
	rows, err := r.sql.QueryContext(ctx, query, groupID, pq.Array(accountIDs), pq.Array(cleanModels), since.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query recent channel monitor traffic: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.ChannelMonitorTrafficSample, 0)
	for rows.Next() {
		var sample service.ChannelMonitorTrafficSample
		var firstToken sql.NullInt64
		if err := rows.Scan(&sample.AccountID, &sample.Model, &sample.DurationMs, &firstToken, &sample.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent channel monitor traffic: %w", err)
		}
		if firstToken.Valid && firstToken.Int64 > 0 {
			sample.FirstTokenMs = int(firstToken.Int64)
		}
		out = append(out, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent channel monitor traffic: %w", err)
	}
	return out, nil
}
