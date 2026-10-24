package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type upstreamRatioSyncEnabledRequest struct {
	Enabled *bool  `json:"enabled" binding:"required"`
	Group   string `json:"group,omitempty"`
}

type upstreamRatioSyncBatchRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
}

// GetUpstreamRatioSyncSettings returns the global runner settings.
func (h *AccountHandler) GetUpstreamRatioSyncSettings(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	settings, err := h.upstreamRatioSync.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateUpstreamRatioSyncSettings persists the global runner settings.
func (h *AccountHandler) UpdateUpstreamRatioSyncSettings(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	var req service.UpstreamRatioSyncSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.upstreamRatioSync.UpdateSettings(c.Request.Context(), &req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings, err := h.upstreamRatioSync.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// SetUpstreamRatioSyncEnabled flips the per-account switch and optionally
// stores the new-api group name used to resolve group_ratio.
func (h *AccountHandler) SetUpstreamRatioSyncEnabled(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req upstreamRatioSyncEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.Group != "" {
		if err := h.upstreamRatioSync.SetAccountGroup(c.Request.Context(), accountID, req.Group); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	if err := h.upstreamRatioSync.SetAccountEnabled(c.Request.Context(), accountID, *req.Enabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": accountID, "enabled": *req.Enabled})
}

// SyncUpstreamRatio triggers one manual sync for a single account.
func (h *AccountHandler) SyncUpstreamRatio(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	snapshot, err := h.upstreamRatioSync.SyncAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, service.UpstreamRatioSyncResult{AccountID: accountID, Snapshot: snapshot})
}

// SyncUpstreamRatioBatch triggers one manual sync for up to 20 accounts.
func (h *AccountHandler) SyncUpstreamRatioBatch(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	var req upstreamRatioSyncBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 || len(req.AccountIDs) > service.UpstreamBillingProbeMaxBatchSize {
		response.BadRequest(c, "account_ids must contain between 1 and 20 items")
		return
	}
	seen := make(map[int64]struct{}, len(req.AccountIDs))
	accountIDs := make([]int64, 0, len(req.AccountIDs))
	for _, accountID := range req.AccountIDs {
		if accountID <= 0 {
			response.BadRequest(c, "account_ids must contain positive IDs")
			return
		}
		if _, exists := seen[accountID]; exists {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	results := h.upstreamRatioSync.SyncAccounts(c.Request.Context(), accountIDs)
	response.Success(c, gin.H{"results": results})
}

// GetUpstreamRatioSyncResult returns the sync snapshot, latest ratio table
// and recent history for one account.
func (h *AccountHandler) GetUpstreamRatioSyncResult(c *gin.Context) {
	if h.upstreamRatioSync == nil {
		response.ErrorFrom(c, service.ErrUpstreamRatioSyncUnavailable)
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	snapshot, rows, history, err := h.upstreamRatioSync.GetAccountResult(
		c.Request.Context(), accountID, c.Query("search"), limit,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"snapshot": snapshot,
		"models":   rows,
		"history":  history,
	})
}
