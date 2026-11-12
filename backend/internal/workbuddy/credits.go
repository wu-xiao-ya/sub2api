package workbuddy

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *server) fetchCredits(ctx context.Context, account credential) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	invalid := status.Error(codes.Unavailable, "invalid WorkBuddy credit response")
	if account.EnterpriseID != "" {
		data, err := s.document(ctx, http.MethodPost, s.billing+"/billing/meter/get-enterprise-user-usage", &account, []byte("{}"))
		if err != nil {
			return nil, err
		}
		if inner, ok := data["Data"].(map[string]any); ok {
			data = inner
		}
		limit, hasLimit := creditNumber(data["limitNum"])
		used, hasUsed := creditNumber(data["credit"])
		if !hasLimit || !hasUsed || limit < 0 || used < 0 {
			return nil, invalid
		}
		result := map[string]any{"remaining": math.Max(0, limit-used), "limit": limit}
		if reset, ok := data["cycleResetTime"].(string); ok && len(reset) <= 40 {
			result["cycle_reset_time"] = reset
		}
		return result, nil
	}

	const pageSize = 100
	now := s.now().In(time.FixedZone("Asia/Shanghai", 8*3600))
	const layout = "2006-01-02 15:04:05"
	total := 0.0
	for page := 1; page <= 20; page++ {
		body, _ := json.Marshal(map[string]any{
			"PageNumber": page, "PageSize": pageSize, "ProductCode": "p_tcaca", "Status": []int{0, 3},
			"PackageEndTimeRangeBegin": now.Format(layout), "PackageEndTimeRangeEnd": now.AddDate(101, 0, 0).Format(layout),
		})
		data, err := s.document(ctx, http.MethodPost, s.billing+"/v2/billing/meter/get-user-resource", &account, body)
		if err != nil {
			return nil, err
		}
		wrapper, _ := data["Response"].(map[string]any)
		inner, _ := wrapper["Data"].(map[string]any)
		accounts, ok := inner["Accounts"].([]any)
		if !ok || wrapper["Error"] != nil {
			return nil, invalid
		}
		for _, raw := range accounts {
			row, _ := raw.(map[string]any)
			remaining, ok := creditCapacity(row, "CycleCapacityRemain")
			if !ok {
				remaining, ok = creditCapacity(row, "CapacityRemain")
			}
			if !ok {
				return nil, invalid
			}
			total += math.Max(0, remaining)
		}
		if len(accounts) < pageSize {
			return map[string]any{"remaining": total}, nil
		}
	}
	return nil, invalid
}

func creditCapacity(row map[string]any, field string) (float64, bool) {
	if value, ok := creditNumber(row[field+"Precise"]); ok {
		return value, true
	}
	return creditNumber(row[field])
}

func creditNumber(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}
