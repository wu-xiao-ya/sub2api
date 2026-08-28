package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const monitoringUserEmail = "sub2api-channel-monitor@local.invalid"

var monitoringKeyPlatforms = map[string]struct{}{
	PlatformDeepSeek: {},
	PlatformKimi:     {},
	PlatformGLM:      {},
	PlatformQwen:     {},
	PlatformMiniMax:  {},
	PlatformMiMo:     {},
	PlatformHunyuan:  {},
}

func isMonitoringKeyPlatform(platform string) bool {
	_, ok := monitoringKeyPlatforms[strings.ToLower(strings.TrimSpace(platform))]
	return ok
}

func (s *APIKeyService) listMonitoringUsers(ctx context.Context) ([]User, error) {
	monitoring := true
	noSubscriptions := false
	users, _, err := s.userRepo.ListWithFilters(ctx, pagination.PaginationParams{
		Page: 1, PageSize: 100, SortBy: "id", SortOrder: "asc",
	}, UserListFilters{
		IsMonitoringUser:     &monitoring,
		IncludeSubscriptions: &noSubscriptions,
	})
	if err != nil {
		return nil, fmt.Errorf("list monitoring users: %w", err)
	}
	return users, nil
}

func (s *APIKeyService) ensureMonitoringUser(ctx context.Context, groupIDs []int64) (*User, error) {
	users, err := s.listMonitoringUsers(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) > 1 {
		return nil, fmt.Errorf("multiple monitoring users exist; keep exactly one user marked for channel monitoring")
	}
	if len(users) == 1 {
		if !users[0].IsActive() {
			return nil, fmt.Errorf("the station monitoring user is inactive")
		}
		return &users[0], nil
	}

	password, err := s.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate monitoring user password: %w", err)
	}
	user := &User{
		Email:            monitoringUserEmail,
		Username:         "Sub2API ????",
		Notes:            "???????????????????",
		Role:             RoleUser,
		Balance:          0,
		Concurrency:      0,
		RPMLimit:         0,
		Status:           StatusActive,
		IsMonitoringUser: true,
		AllowedGroups:    append([]int64(nil), groupIDs...),
	}
	if err := user.SetPassword(password); err != nil {
		return nil, fmt.Errorf("hash monitoring user password: %w", err)
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		// A concurrent initializer may have won the email race. Re-read the
		// canonical monitoring user instead of creating a second identity.
		if existing, getErr := s.userRepo.GetByEmail(ctx, monitoringUserEmail); getErr == nil &&
			existing != nil && existing.IsMonitoringUser {
			return existing, nil
		}
		return nil, fmt.Errorf("create monitoring user: %w", err)
	}
	return user, nil
}

func (s *APIKeyService) loadMonitoringGroups(ctx context.Context, groupIDs []int64) ([]*Group, error) {
	seen := make(map[int64]struct{}, len(groupIDs))
	groups := make([]*Group, 0, len(groupIDs))
	for _, id := range groupIDs {
		if id <= 0 {
			return nil, fmt.Errorf("monitoring group id must be positive")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		group, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load monitoring group %d: %w", id, err)
		}
		if group == nil || !group.IsActive() {
			return nil, fmt.Errorf("monitoring group %d is not active", id)
		}
		if !isMonitoringKeyPlatform(group.Platform) {
			return nil, fmt.Errorf("monitoring group %d uses unsupported platform %q", id, group.Platform)
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups, nil
}

func (s *APIKeyService) listAllKeysForUser(ctx context.Context, userID int64) ([]APIKey, error) {
	if repo, ok := s.apiKeyRepo.(apiKeyAllByUserIDLister); ok {
		return repo.ListAllByUserID(ctx, userID, APIKeyListFilters{})
	}
	keys, _, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{
		Page: 1, PageSize: 1000,
	}, APIKeyListFilters{})
	return keys, err
}

func monitoringKeyInfo(key *APIKey) (InternalMonitorKey, bool) {
	if key == nil || key.Group == nil || key.GroupID == nil {
		return InternalMonitorKey{}, false
	}
	return InternalMonitorKey{
		ID: key.ID, Name: key.Name, UserID: key.UserID,
		GroupID: *key.GroupID, GroupName: key.Group.Name, Provider: key.Group.Platform,
		Status: key.Status, ExpiresAt: key.ExpiresAt,
	}, true
}

func (s *APIKeyService) ListMonitoringMonitorKeys(ctx context.Context) ([]InternalMonitorKey, error) {
	users, err := s.listMonitoringUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InternalMonitorKey, 0)
	for i := range users {
		keys, keyErr := s.listAllKeysForUser(ctx, users[i].ID)
		if keyErr != nil {
			return nil, fmt.Errorf("list monitoring user keys: %w", keyErr)
		}
		for j := range keys {
			if info, ok := monitoringKeyInfo(&keys[j]); ok {
				out = append(out, info)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GroupID != out[j].GroupID {
			return out[i].GroupID < out[j].GroupID
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *APIKeyService) EnsureMonitoringMonitorKeys(ctx context.Context, groupIDs []int64) (*InternalMonitorKeyEnsureResult, error) {
	if len(groupIDs) == 0 {
		return nil, fmt.Errorf("at least one monitoring group is required")
	}
	groups, err := s.loadMonitoringGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	normalizedIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		normalizedIDs = append(normalizedIDs, group.ID)
	}
	user, err := s.ensureMonitoringUser(ctx, normalizedIDs)
	if err != nil {
		return nil, err
	}
	for _, groupID := range normalizedIDs {
		if err := s.userRepo.AddGroupToAllowedGroups(ctx, user.ID, groupID); err != nil {
			return nil, fmt.Errorf("allow monitoring user group %d: %w", groupID, err)
		}
	}

	keys, err := s.listAllKeysForUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list existing monitoring keys: %w", err)
	}
	existingByGroup := make(map[int64]*APIKey)
	for i := range keys {
		if keys[i].GroupID != nil {
			if _, exists := existingByGroup[*keys[i].GroupID]; !exists {
				existingByGroup[*keys[i].GroupID] = &keys[i]
			}
		}
	}

	result := &InternalMonitorKeyEnsureResult{
		UserID: user.ID, UserEmail: user.Email,
		Items: make([]InternalMonitorKeyEnsureItem, 0, len(groups)),
	}
	for _, group := range groups {
		if existing := existingByGroup[group.ID]; existing != nil {
			if existing.Status != StatusAPIKeyActive || existing.IsExpired() {
				return nil, fmt.Errorf("existing monitoring key for group %d is inactive or expired", group.ID)
			}
			info, ok := monitoringKeyInfo(existing)
			if !ok {
				return nil, fmt.Errorf("existing monitoring key for group %d is invalid", group.ID)
			}
			result.Items = append(result.Items, InternalMonitorKeyEnsureItem{InternalMonitorKey: info})
			continue
		}
		plainKey, keyErr := s.GenerateKey()
		if keyErr != nil {
			return nil, fmt.Errorf("generate monitoring key for group %d: %w", group.ID, keyErr)
		}
		key := &APIKey{
			UserID: user.ID, Key: plainKey, Name: fmt.Sprintf("???? ? %s", group.Name),
			GroupID: &group.ID, Status: StatusAPIKeyActive,
		}
		if err := s.apiKeyRepo.Create(ctx, key); err != nil {
			return nil, fmt.Errorf("create monitoring key for group %d: %w", group.ID, err)
		}
		key.User = user
		key.Group = group
		info, ok := monitoringKeyInfo(key)
		if !ok {
			return nil, fmt.Errorf("created monitoring key for group %d is invalid", group.ID)
		}
		result.Items = append(result.Items, InternalMonitorKeyEnsureItem{
			InternalMonitorKey: info, PlainKey: plainKey, Created: true,
		})
		s.InvalidateAuthCacheByKey(ctx, plainKey)
	}
	return result, nil
}
