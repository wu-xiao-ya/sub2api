package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *opsRepository) GetUpstreamErrorAttribution(ctx context.Context, filter *service.OpsDashboardFilter) (*service.OpsUpstreamAttributionResponse, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if filter == nil {
		return nil, fmt.Errorf("nil ops attribution filter")
	}

	args := []any{filter.StartTime, filter.EndTime}
	where := "WHERE e.created_at >= $1 AND e.created_at < $2"
	nextArg := 3
	if filter.Platform != "" {
		where += fmt.Sprintf(" AND e.platform = $%d", nextArg)
		args = append(args, filter.Platform)
		nextArg++
	}
	if filter.GroupID != nil {
		where += fmt.Sprintf(" AND e.group_id = $%d", nextArg)
		args = append(args, *filter.GroupID)
		nextArg++
	}

	// Include both ordinary upstream HTTP failures and in-band stream errors.
	// The latter can have wire status 200, so filtering only on status_code would
	// silently lose the exact class of failure reported by Codex clients.
	where += ` AND (
		e.error_owner = 'provider'
		OR e.error_phase IN ('upstream', 'network')
		OR e.upstream_status_code IS NOT NULL
	)`
	where += " AND COALESCE(e.is_count_tokens, false) = false"

	q := `
WITH candidates AS (
	SELECT
		e.group_id,
		COALESCE(g.name, '') AS group_name,
		e.account_id,
		COALESCE(a.name, '') AS account_name,
		COALESCE(e.upstream_status_code, e.status_code, 0) AS status_code,
		LOWER(COALESCE(e.upstream_error_message, e.error_message, '')) AS message,
		LOWER(COALESCE(e.error_phase, '')) AS phase,
		LOWER(COALESCE(e.error_type, '')) AS error_type,
		COALESCE(e.stream, false) AS stream,
		COALESCE(e.upstream_latency_ms, 0) AS upstream_latency_ms,
		e.created_at,
		COALESCE(
			NULLIF(e.upstream_endpoint, ''),
			NULLIF(e.inbound_endpoint, ''),
			e.request_path,
			''
		) AS endpoint
	FROM ops_error_logs e
	LEFT JOIN accounts a ON a.id = e.account_id
	LEFT JOIN groups g ON g.id = e.group_id
	` + where + `
),
grouped AS (
	SELECT
		group_id,
		group_name,
		account_id,
		account_name,
		COUNT(*) AS total,
		COUNT(*) FILTER (
			WHERE status_code IN (502, 503, 504, 529)
				OR message LIKE '%overload%'
				OR message LIKE '%overloaded%'
				OR message LIKE '%temporarily unavailable%'
		) AS overload,
		COUNT(*) FILTER (
			WHERE status_code = 429 OR error_type LIKE '%rate_limit%'
		) AS rate_limit,
		COUNT(*) FILTER (
			WHERE status_code >= 500
				AND NOT (
					status_code IN (502, 503, 504, 529)
					OR message LIKE '%overload%'
					OR message LIKE '%overloaded%'
					OR message LIKE '%temporarily unavailable%'
				)
		) AS server_error,
		COUNT(*) FILTER (
			WHERE phase = 'network'
				OR message ~ '(eof|connection reset|broken pipe|timeout|connection refused|context canceled)'
		) AS transport,
		COUNT(*) FILTER (
			WHERE stream
				AND (
					phase = 'network'
					OR error_type LIKE '%stream%'
					OR message LIKE '%stream%'
					OR message ~ '(eof|connection reset|broken pipe|timeout)'
				)
		) AS stream_failure,
		ROUND(AVG(NULLIF(upstream_latency_ms, 0)))::bigint AS average_upstream_latency_ms,
		MAX(created_at) AS last_error_at,
		(ARRAY_AGG(status_code ORDER BY created_at DESC))[1] AS last_status_code,
		(ARRAY_AGG(message ORDER BY created_at DESC))[1] AS last_message,
		(ARRAY_AGG(endpoint ORDER BY created_at DESC))[1] AS last_endpoint
	FROM candidates
	GROUP BY group_id, group_name, account_id, account_name
)
SELECT
	group_id,
	group_name,
	account_id,
	account_name,
	total,
	overload,
	rate_limit,
	server_error,
	transport,
	stream_failure,
	average_upstream_latency_ms,
	last_error_at,
	last_status_code,
	last_message,
	last_endpoint
FROM grouped
ORDER BY total DESC, overload DESC, last_error_at DESC
LIMIT 100`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := &service.OpsUpstreamAttributionResponse{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
		Platform:  filter.Platform,
		GroupID:   filter.GroupID,
		Items:     make([]*service.OpsUpstreamAttributionItem, 0, 16),
	}

	for rows.Next() {
		item := &service.OpsUpstreamAttributionItem{}
		var groupID, accountID sql.NullInt64
		var groupName, accountName string
		var averageLatency sql.NullInt64
		var lastErrorAt sql.NullTime
		var lastMessage, lastEndpoint sql.NullString
		if err := rows.Scan(
			&groupID,
			&groupName,
			&accountID,
			&accountName,
			&item.Total,
			&item.Overload,
			&item.RateLimit,
			&item.ServerError,
			&item.Transport,
			&item.StreamFailure,
			&averageLatency,
			&lastErrorAt,
			&item.LastStatusCode,
			&lastMessage,
			&lastEndpoint,
		); err != nil {
			return nil, err
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		if accountID.Valid {
			v := accountID.Int64
			item.AccountID = &v
		}
		item.GroupName = groupName
		item.AccountName = accountName
		if averageLatency.Valid {
			v := averageLatency.Int64
			item.AverageUpstreamLatencyMs = &v
		}
		if lastErrorAt.Valid {
			item.LastErrorAt = lastErrorAt.Time
		}
		item.LastMessage = lastMessage.String
		item.LastEndpoint = lastEndpoint.String
		result.Total += item.Total
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
