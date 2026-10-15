package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func (s *GatewayService) GroupSupportsRequestedModel(ctx context.Context, group *Group, requestedModel string) bool {
	if group == nil || !group.IsActive() {
		return false
	}
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		return false
	}
	if group.CustomModelsListEnabled() && !modelIDInList(group.ModelsListConfig.Models, model) {
		return false
	}
	if s != nil && s.channelService != nil && s.channelService.IsModelRestricted(ctx, group.ID, model) {
		return false
	}
	var available []string
	if s != nil && s.accountRepo != nil {
		available = s.GetAvailableModels(ctx, &group.ID, group.Platform)
	}
	if len(available) > 0 {
		return modelIDInList(available, model)
	}
	defaults := platformDefaultModelIDs(group.Platform)
	if len(defaults) == 0 {
		return true
	}
	return modelIDInList(defaults, model)
}

func (s *GatewayService) ListModelsForGroup(ctx context.Context, group *Group) []string {
	if group == nil {
		return nil
	}
	available := s.GetAvailableModels(ctx, &group.ID, group.Platform)
	defaults := platformDefaultModelIDs(group.Platform)
	if group.CustomModelsListEnabled() {
		source := available
		if group.Platform == PlatformAnthropic && len(available) > 0 {
			source = mergeUniqueModelIDs(available, defaults)
		}
		if len(source) == 0 {
			source = defaults
		}
		return filterModelIDsByAllowList(source, group.ModelsListConfig.Models)
	}
	if len(available) > 0 {
		return append([]string(nil), available...)
	}
	return append([]string(nil), defaults...)
}

func platformDefaultModelIDs(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformAnthropic:
		ids := make([]string, 0, len(claude.DefaultModels)+len(antigravity.DefaultModels()))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		for _, model := range antigravity.DefaultModels() {
			ids = append(ids, model.ID)
		}
		return mergeUniqueModelIDs(ids, nil)
	case PlatformGrok:
		return xai.DefaultModelIDs()
	case PlatformDeepSeek, PlatformKimi, PlatformGLM, PlatformQwen, PlatformMiniMax, PlatformMiMo, PlatformHunyuan:
		return DefaultOpenAICompatibleModelIDs(platform)
	default:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

func modelIDInList(models []string, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return false
	}
	for _, model := range models {
		if strings.TrimSpace(model) == requested {
			return true
		}
	}
	return false
}

func mergeUniqueModelIDs(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	out := make([]string, 0, len(primary)+len(secondary))
	for _, models := range [][]string{primary, secondary} {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			out = append(out, model)
		}
	}
	return out
}

func filterModelIDsByAllowList(source, allow []string) []string {
	if len(allow) == 0 {
		return append([]string(nil), source...)
	}
	allowed := make(map[string]struct{}, len(allow))
	for _, model := range allow {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		allowed[model] = struct{}{}
	}
	out := make([]string, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	for _, model := range source {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := allowed[model]; !ok {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}
