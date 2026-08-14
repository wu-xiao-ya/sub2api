package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GetImageUpstreamCost returns the upstream cost used by admin cost/profit
// reporting for one generated image.
// GET /api/v1/admin/settings/image-upstream-cost
func (h *SettingHandler) GetImageUpstreamCost(c *gin.Context) {
	response.Success(c, h.settingService.GetImageUpstreamCostSettings(c.Request.Context()))
}

type updateImageUpstreamCostRequest struct {
	CostPerImage     *float64                                    `json:"cost_per_image"`
	AccountOverrides *[]service.ImageUpstreamCostAccountOverride `json:"account_overrides"`
}

// UpdateImageUpstreamCost updates the upstream cost used by cost/profit
// reporting for one generated image. This does not change user-facing image
// prices or historical usage-log amounts.
// PUT /api/v1/admin/settings/image-upstream-cost
func (h *SettingHandler) UpdateImageUpstreamCost(c *gin.Context) {
	var req updateImageUpstreamCostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.CostPerImage == nil && req.AccountOverrides == nil {
		response.BadRequest(c, "cost_per_image or account_overrides is required")
		return
	}
	if err := h.settingService.UpdateImageUpstreamCostSettings(c.Request.Context(), req.CostPerImage, req.AccountOverrides); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.settingService.GetImageUpstreamCostSettings(c.Request.Context()))
}
