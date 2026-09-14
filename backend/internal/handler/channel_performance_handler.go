package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AvailableChannelHandler) Performance(c *gin.Context)       { h.performance(c, false) }
func (h *AvailableChannelHandler) PerformanceDetail(c *gin.Context) { h.performance(c, true) }

func (h *AvailableChannelHandler) performance(c *gin.Context, detail bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.featureEnabled(c) {
		response.NotFound(c, "Available channels are disabled")
		return
	}
	if h.performanceService == nil {
		response.Error(c, 503, "Performance data unavailable")
		return
	}
	f := service.ChannelPerformanceFilter{Range: c.Query("range"), Model: c.Query("model"), ServiceTier: c.Query("service_tier"), ReasoningEffort: c.Query("reasoning_effort")}
	if raw := c.Query("group_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		f.GroupID = id
	}
	if raw := c.Query("stream"); raw != "" {
		if raw != "true" && raw != "false" {
			response.BadRequest(c, "Invalid stream")
			return
		}
		value := raw == "true"
		f.Stream = &value
	}
	if err := f.Normalize(time.Now()); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	userGroups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	allowed := make(map[int64]struct{}, len(userGroups))
	for _, group := range userGroups {
		allowed[group.ID] = struct{}{}
	}
	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	scopes := availablePerformanceScopes(channels, allowed)
	if detail {
		key := c.Query("key")
		if key == "" || len(key) > 1024 {
			response.BadRequest(c, "Model card key is required")
			return
		}
		selected := []service.ChannelPerformanceScope{}
		for _, scope := range scopes {
			if scope.Key == key {
				selected = append(selected, scope)
			}
		}
		if len(selected) == 0 {
			response.NotFound(c, "Model not available")
			return
		}
		scopes = selected
		f.Model = selected[0].Model
	}
	// A supplied group can only narrow server-derived scope. Return no details
	// about whether an inaccessible group exists.
	if f.GroupID != 0 {
		visible := false
		for _, scope := range scopes {
			if _, ok := scope.Groups[f.GroupID]; ok {
				visible = true
			}
		}
		if !visible {
			response.NotFound(c, "Group not available")
			return
		}
	}
	result, err := h.performanceService.Query(c.Request.Context(), f, scopes, detail)
	if err != nil {
		response.Error(c, 503, "Performance data temporarily unavailable")
		return
	}
	response.Success(c, result)
}

func availablePerformanceScopes(channels []service.AvailableChannel, allowed map[int64]struct{}) []service.ChannelPerformanceScope {
	scopes := []service.ChannelPerformanceScope{}
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		for _, section := range buildPlatformSections(ch, filterUserVisibleGroups(ch.Groups, allowed)) {
			groups := make(map[int64]string, len(section.Groups))
			for _, g := range section.Groups {
				groups[g.ID] = g.Name
			}
			for _, model := range section.SupportedModels {
				scopes = append(scopes, service.ChannelPerformanceScope{
					Key:      ch.Name + "\x00" + section.Platform + "\x00" + model.Name,
					Platform: section.Platform, Model: model.Name, Groups: groups,
				})
			}
		}
	}
	return scopes
}
