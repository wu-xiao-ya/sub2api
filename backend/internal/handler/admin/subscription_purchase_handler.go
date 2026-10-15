package admin

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionPurchaseHandler manages the canonical subscription purchase
// snapshots. The retired native subscription handler is intentionally kept
// separate so old IDs and DTOs cannot be mixed with purchase IDs.
type SubscriptionPurchaseHandler struct {
	subscriptionService *service.SubscriptionService
}

func NewSubscriptionPurchaseHandler(subscriptionService *service.SubscriptionService) *SubscriptionPurchaseHandler {
	return &SubscriptionPurchaseHandler{subscriptionService: subscriptionService}
}

type grantPurchaseRequest struct {
	UserID int64  `json:"user_id" binding:"required"`
	PlanID int64  `json:"plan_id" binding:"required"`
	Notes  string `json:"notes"`
}

type bulkGrantPurchaseRequest struct {
	UserIDs []int64 `json:"user_ids" binding:"required,min=1"`
	PlanID  int64   `json:"plan_id" binding:"required"`
	Notes   string  `json:"notes"`
}

type adjustPurchaseRequest struct {
	Days int `json:"days" binding:"required,min=-36500,max=36500"`
}

type resetPurchaseQuotaRequest struct {
	Daily   bool `json:"daily"`
	Weekly  bool `json:"weekly"`
	Monthly bool `json:"monthly"`
}

func (h *SubscriptionPurchaseHandler) List(c *gin.Context) {
	query := service.AdminPurchaseListQuery{}
	query.Page, query.PageSize = response.ParsePagination(c)
	query.Status = c.Query("status")
	query.Platform = c.Query("platform")
	query.Keyword = c.Query("keyword")
	query.UserID = optionalInt64Query(c, "user_id")
	query.PlanID = optionalInt64Query(c, "plan_id")
	result, err := h.subscriptionService.ListPurchaseRecords(c.Request.Context(), query)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *SubscriptionPurchaseHandler) Get(c *gin.Context) {
	id, err := purchaseIDParam(c)
	if err != nil {
		response.BadRequest(c, "Invalid purchase ID")
		return
	}
	item, err := h.subscriptionService.GetPurchaseRecord(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SubscriptionPurchaseHandler) Grant(c *gin.Context) {
	var req grantPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.grant", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.GrantPurchaseFromPlan(ctx, req.UserID, req.PlanID, req.Notes)
	})
}

func (h *SubscriptionPurchaseHandler) BulkGrant(c *gin.Context) {
	var req bulkGrantPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.bulk_grant", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.BulkGrantPurchaseFromPlan(ctx, req.UserIDs, req.PlanID, req.Notes)
	})
}

func (h *SubscriptionPurchaseHandler) Extend(c *gin.Context) {
	id, err := purchaseIDParam(c)
	if err != nil {
		response.BadRequest(c, "Invalid purchase ID")
		return
	}
	var req adjustPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.extend", struct {
		ID   int64                 `json:"id"`
		Body adjustPurchaseRequest `json:"body"`
	}{id, req}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.ExtendPurchase(ctx, id, req.Days)
	})
}

func (h *SubscriptionPurchaseHandler) Revoke(c *gin.Context) {
	id, err := purchaseIDParam(c)
	if err != nil {
		response.BadRequest(c, "Invalid purchase ID")
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.revoke", map[string]any{"id": id}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.RevokeAdminPurchase(ctx, id)
	})
}

func (h *SubscriptionPurchaseHandler) Restore(c *gin.Context) {
	id, err := purchaseIDParam(c)
	if err != nil {
		response.BadRequest(c, "Invalid purchase ID")
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.restore", map[string]any{"id": id}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.RestoreAdminPurchase(ctx, id)
	})
}

func (h *SubscriptionPurchaseHandler) ResetQuota(c *gin.Context) {
	id, err := purchaseIDParam(c)
	if err != nil {
		response.BadRequest(c, "Invalid purchase ID")
		return
	}
	var req resetPurchaseQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAdminIdempotentJSON(c, "admin.subscription_purchases.reset_quota", struct {
		ID   int64                     `json:"id"`
		Body resetPurchaseQuotaRequest `json:"body"`
	}{id, req}, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.subscriptionService.ResetPurchaseQuota(ctx, id, req.Daily, req.Weekly, req.Monthly)
	})
}

func optionalInt64Query(c *gin.Context, name string) *int64 {
	raw := c.Query(name)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

func purchaseIDParam(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}
