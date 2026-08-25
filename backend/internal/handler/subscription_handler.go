package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SubscriptionHandler exposes Starlight purchase entitlements to users.
type SubscriptionHandler struct {
	subscriptionService *service.SubscriptionService
}

type SharedSubscriptionResponse struct {
	ID                     int64                             `json:"id"`
	Name                   string                            `json:"name"`
	TierCode               string                            `json:"tier_code"`
	StartsAt               time.Time                         `json:"starts_at"`
	ExpiresAt              time.Time                         `json:"expires_at"`
	Status                 string                            `json:"status"`
	ConcurrencyEntitlement int                               `json:"concurrency_entitlement"`
	LifetimeQuotaUSD       float64                           `json:"lifetime_quota_usd"`
	DailyQuotaUSD          float64                           `json:"daily_quota_usd"`
	WeeklyQuotaUSD         float64                           `json:"weekly_quota_usd"`
	MonthlyQuotaUSD        float64                           `json:"monthly_quota_usd"`
	LifetimeUsageUSD       float64                           `json:"lifetime_usage_usd"`
	DailyUsageUSD          float64                           `json:"daily_usage_usd"`
	WeeklyUsageUSD         float64                           `json:"weekly_usage_usd"`
	MonthlyUsageUSD        float64                           `json:"monthly_usage_usd"`
	BalanceTopupEnabled    bool                              `json:"balance_topup_enabled"`
	Groups                 []service.SharedSubscriptionGroup `json:"groups"`
}

// NewSubscriptionHandler creates a new user subscription handler
func NewSubscriptionHandler(subscriptionService *service.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{
		subscriptionService: subscriptionService,
	}
}

// ListShared returns immutable purchase snapshots for the current user.
func (h *SubscriptionHandler) ListShared(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 401, "authentication required")
		return
	}
	items, err := h.subscriptionService.ListActiveSharedSubscriptions(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]SharedSubscriptionResponse, 0, len(items))
	for i := range items {
		item := items[i]
		groups, groupErr := h.subscriptionService.ListSharedSubscriptionGroups(c.Request.Context(), item.ID)
		if groupErr != nil {
			response.ErrorFrom(c, groupErr)
			return
		}
		out = append(out, SharedSubscriptionResponse{
			ID: item.ID, Name: item.Name, TierCode: item.TierCode,
			StartsAt: item.StartsAt, ExpiresAt: item.ExpiresAt, Status: item.Status,
			ConcurrencyEntitlement: item.ConcurrencyEntitlement,
			LifetimeQuotaUSD:       item.LifetimeQuotaUSD, DailyQuotaUSD: item.DailyQuotaUSD,
			WeeklyQuotaUSD: item.WeeklyQuotaUSD, MonthlyQuotaUSD: item.MonthlyQuotaUSD,
			LifetimeUsageUSD: item.LifetimeUsageUSD, DailyUsageUSD: item.DailyUsageUSD,
			WeeklyUsageUSD: item.WeeklyUsageUSD, MonthlyUsageUSD: item.MonthlyUsageUSD,
			BalanceTopupEnabled: item.BalanceTopupEnabled, Groups: groups,
		})
	}
	response.Success(c, out)
}

func (h *SubscriptionHandler) SetSharedBalanceTopup(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 401, "authentication required")
		return
	}
	purchaseID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || purchaseID <= 0 {
		response.Error(c, 400, "invalid subscription purchase id")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.Error(c, 400, "enabled is required")
		return
	}
	if err := h.subscriptionService.SetSharedSubscriptionBalanceTopup(
		c.Request.Context(), subject.UserID, purchaseID, *req.Enabled,
	); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": purchaseID, "balance_topup_enabled": *req.Enabled})
}
