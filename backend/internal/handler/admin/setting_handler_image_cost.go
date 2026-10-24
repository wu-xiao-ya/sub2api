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

// GetImageUpstreamCostCandidates returns the image models and accounts that
// recently generated images, so the settings UI can offer pickers instead of
// raw ID and model-name text inputs.
// GET /api/v1/admin/settings/image-upstream-cost/candidates
func (h *SettingHandler) GetImageUpstreamCostCandidates(c *gin.Context) {
	response.Success(c, h.settingService.GetImageUpstreamCostCandidates(c.Request.Context()))
}

type updateImageUpstreamCostRequest struct {
	CostPerImage               *float64                                         `json:"cost_per_image"`
	AccountOverrides           *[]service.ImageUpstreamCostAccountOverride      `json:"account_overrides"`
	ModelOverrides             *[]service.ImageUpstreamCostModelOverride        `json:"model_overrides"`
	AccountModelOverrides      *[]service.ImageUpstreamCostAccountModelOverride `json:"account_model_overrides"`
	IgnoreUpstreamRateSnapshot *bool                                            `json:"ignore_upstream_rate_snapshot"`
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
	if req.CostPerImage == nil && req.AccountOverrides == nil && req.ModelOverrides == nil &&
		req.AccountModelOverrides == nil && req.IgnoreUpstreamRateSnapshot == nil {
		response.BadRequest(c, "at least one image upstream cost field is required")
		return
	}
	if err := h.settingService.UpdateImageUpstreamCostSettings(
		c.Request.Context(),
		req.CostPerImage,
		req.AccountOverrides,
		req.ModelOverrides,
		req.AccountModelOverrides,
		req.IgnoreUpstreamRateSnapshot,
	); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.settingService.GetImageUpstreamCostSettings(c.Request.Context()))
}
