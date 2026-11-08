package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

const WorkBuddyModelsExtraKey = "workbuddy_enabled_models"

func (a *Account) WorkBuddyModelSelection() ([]string, bool) {
	if a == nil || a.Type != AccountTypePlugin || a.Platform != PlatformDeepseek {
		return nil, false
	}
	raw, exists := a.Extra[WorkBuddyModelsExtraKey]
	if !exists {
		return nil, false
	}
	switch values := raw.(type) {
	case []string:
		return values, true
	case []any:
		models := make([]string, 0, len(values))
		for _, value := range values {
			if model, ok := value.(string); ok {
				models = append(models, model)
			}
		}
		return models, true
	default:
		return nil, true
	}
}

func (a *Account) allowsSelectedWorkBuddyModel(model string) bool {
	models, configured := a.WorkBuddyModelSelection()
	return !configured || slices.Contains(models, model) || slices.Contains(models, normalizeRequestedModelForLookup(a.Platform, model))
}

func (m *PluginManager) RefreshWorkBuddyCatalog(ctx context.Context, account *Account) ([]string, error) {
	binding, err := m.providerAccountBinding(ctx, account)
	if err != nil {
		return nil, err
	}
	p, err := m.repo.GetByID(ctx, binding.PluginID)
	if err != nil || !isBuiltinWorkBuddy(p) {
		return nil, errors.New("model selection is only available for built-in WorkBuddy")
	}
	if _, err := m.Authorize(ctx, binding.PluginID, "prepare", binding.IdentityRef, ""); err != nil {
		return nil, err
	}
	raw, err := m.Authorize(ctx, binding.PluginID, "catalog", binding.IdentityRef, "")
	if err != nil {
		return nil, err
	}
	var result struct {
		IdentityRef string   `json:"identity_ref"`
		Models      []string `json:"models"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IdentityRef != binding.IdentityRef || result.Models == nil {
		return nil, errors.New("invalid WorkBuddy model catalog")
	}
	return result.Models, nil
}

func (s *AccountTestService) WorkBuddyCatalog(ctx context.Context, account *Account, refresh bool) ([]string, error) {
	if s == nil || s.pluginManager == nil {
		return nil, errors.New("WorkBuddy is unavailable")
	}
	manager, ok := s.pluginManager.(*PluginManager)
	if !ok {
		return nil, errors.New("WorkBuddy catalog is unavailable")
	}
	if refresh {
		return manager.RefreshWorkBuddyCatalog(ctx, account)
	}
	binding, err := manager.providerAccountBinding(ctx, account)
	if err != nil {
		return nil, err
	}
	p, err := manager.repo.GetByID(ctx, binding.PluginID)
	if err != nil || !isBuiltinWorkBuddy(p) {
		return nil, errors.New("model selection is only available for built-in WorkBuddy")
	}
	return manager.inspectProviderIdentity(ctx, p.ID, binding.IdentityRef)
}
