package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
)

func (h *GatewayHandler) aggregateGroupModels(ctx context.Context, group *service.Group) []string {
	if h == nil || h.gatewayService == nil || group == nil || !group.IsActive() {
		return nil
	}
	var models []string
	if group.Platform == service.PlatformComposite {
		models = h.compositeAvailableModels(ctx, &group.ID, "", true)
	} else {
		models = h.gatewayService.GetAvailableModels(ctx, &group.ID, group.Platform)
		if group.ModelAllowlistEnabled() {
			models = modelListingSource(group.Platform, models, aggregateDefaultModels(group.Platform))
		} else if len(models) == 0 && (group.Platform != service.PlatformOpenAI || models == nil) {
			models = aggregateDefaultModels(group.Platform)
		}
	}
	if group.ModelAllowlistEnabled() {
		models = group.ModelAllowlist.FilterForListing(models)
	}
	return models
}

// AggregateGroupSupportsModel follows the official catalog, allowlist and
// channel restrictions; it never searches accounts outside the member group.
func (h *GatewayHandler) AggregateGroupSupportsModel(ctx context.Context, group *service.Group, model string) bool {
	if group == nil || strings.TrimSpace(model) == "" || !group.ModelAllowlist.Allows(model) {
		return false
	}
	if h.gatewayService.IsModelRestricted(ctx, group.ID, model) {
		return false
	}
	candidates := h.aggregateGroupModels(ctx, group)
	catalog := service.GroupModelAllowlist{Enabled: true, Models: candidates}
	return len(candidates) > 0 && catalog.Allows(model)
}

func (h *GatewayHandler) aggregateModels(ctx context.Context, root *service.APIKey) []string {
	models := make([]string, 0)
	seen := make(map[string]bool)
	for _, member := range root.Members {
		if member == nil || member.UserID != root.UserID || !member.IsMemberUsable() {
			continue
		}
		for _, model := range h.aggregateGroupModels(ctx, member.Group) {
			if seen[model] || h.gatewayService.IsModelRestricted(ctx, member.Group.ID, model) {
				continue
			}
			seen[model] = true
			models = append(models, model)
		}
	}
	return models
}

// CN providers rely on their configured model mappings; never fall back to
// another provider's catalog when one of those mappings is absent.
func aggregateDefaultModels(platform string) []string {
	switch platform {
	case service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformAntigravity, service.PlatformOpenCodeGo, service.PlatformTypeSafe:
		return defaultModelIDsForPlatform(platform)
	}
	return nil
}
