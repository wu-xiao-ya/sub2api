package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type IntelligenceProbeHandler struct {
	probe *service.IntelligenceProbeService
}

func NewIntelligenceProbeHandler(probe *service.IntelligenceProbeService) *IntelligenceProbeHandler {
	return &IntelligenceProbeHandler{probe: probe}
}

// GetConfig returns runner settings plus the configured target list.
func (h *IntelligenceProbeHandler) GetConfig(c *gin.Context) {
	if h.probe == nil {
		response.ErrorFrom(c, service.ErrIntelligenceProbeUnavailable)
		return
	}
	config, err := h.probe.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

type intelligenceProbeConfigRequest struct {
	Settings service.IntelligenceProbeSettings  `json:"settings"`
	Targets  []*service.IntelligenceProbeTarget `json:"targets"`
}

// UpdateConfig validates and persists settings, then replaces the target list.
func (h *IntelligenceProbeHandler) UpdateConfig(c *gin.Context) {
	if h.probe == nil {
		response.ErrorFrom(c, service.ErrIntelligenceProbeUnavailable)
		return
	}
	var req intelligenceProbeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	config, err := h.probe.UpdateConfig(c.Request.Context(), &service.IntelligenceProbeConfig{
		Settings: req.Settings,
		Targets:  req.Targets,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

type intelligenceProbeRunRequest struct {
	TargetID int64 `json:"target_id"`
}

// RunNow starts background probes and returns immediately: a drawing takes
// minutes, and a synchronous response would be killed by browser or proxy
// timeouts, cancelling the probe mid-flight. Results appear in the gallery.
func (h *IntelligenceProbeHandler) RunNow(c *gin.Context) {
	if h.probe == nil {
		response.ErrorFrom(c, service.ErrIntelligenceProbeUnavailable)
		return
	}
	var req intelligenceProbeRunRequest
	_ = c.ShouldBindJSON(&req)
	if req.TargetID > 0 {
		started, err := h.probe.RunTargetBackground(req.TargetID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, gin.H{"started": started})
		return
	}
	count, err := h.probe.RunEnabledBackground()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"started": count > 0, "targets": count})
}

// ListResults returns stored probe results for one target, newest first.
func (h *IntelligenceProbeHandler) ListResults(c *gin.Context) {
	if h.probe == nil {
		response.ErrorFrom(c, service.ErrIntelligenceProbeUnavailable)
		return
	}
	targetID, err := strconv.ParseInt(c.Query("target_id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.BadRequest(c, "target_id is required")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "120"))
	results, err := h.probe.ListResults(c.Request.Context(), targetID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": results})
}

// ListUserResults returns recent results for every enabled target; used by the
// user-facing channel status gallery.
func (h *IntelligenceProbeHandler) ListUserResults(c *gin.Context) {
	if h.probe == nil {
		response.ErrorFrom(c, service.ErrIntelligenceProbeUnavailable)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "120"))
	results, err := h.probe.ListUserResults(c.Request.Context(), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": results})
}
