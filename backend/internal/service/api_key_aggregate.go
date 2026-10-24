package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	APIKeyKindGroup     = "group"
	APIKeyKindAggregate = "aggregate"
	maxAggregateMembers = 32
)

var (
	ErrAggregateMembersRequired = infraerrors.BadRequest("AGGREGATE_MEMBERS_REQUIRED", "aggregate API key requires at least one member key")
	ErrAggregateTooManyMembers  = infraerrors.BadRequest("AGGREGATE_TOO_MANY_MEMBERS", "aggregate API key cannot include more than 32 member keys")
	ErrAggregateNested          = infraerrors.BadRequest("AGGREGATE_NESTED", "aggregate API keys cannot include another aggregate key")
	ErrAggregateDuplicateGroup  = infraerrors.BadRequest("AGGREGATE_DUPLICATE_GROUP", "each member key must belong to a different group")
	ErrAggregateMemberNotFound  = infraerrors.BadRequest("AGGREGATE_MEMBER_NOT_FOUND", "one or more member keys were not found")
	ErrAggregateMemberNotOwned  = infraerrors.Forbidden("AGGREGATE_MEMBER_NOT_OWNED", "member keys must belong to the same user")
	ErrAggregateMemberNoGroup   = infraerrors.BadRequest("AGGREGATE_MEMBER_NO_GROUP", "member keys must be bound to a group")
	ErrAggregateCannotBindGroup = infraerrors.BadRequest("AGGREGATE_CANNOT_BIND_GROUP", "aggregate API keys cannot be bound to a group")
	ErrAggregateNoMatchingMember = infraerrors.BadRequest("AGGREGATE_NO_MATCHING_MEMBER", "no member API key supports the requested model")
	ErrAggregateUnsupportedEndpoint = infraerrors.BadRequest("AGGREGATE_UNSUPPORTED_ENDPOINT", "aggregate API keys are not supported on this endpoint")
	ErrAggregateModelRequired = infraerrors.BadRequest("AGGREGATE_MODEL_REQUIRED", "aggregate API keys require a model to route the request")
	ErrGroupKeyCannotHaveMembers = infraerrors.BadRequest("GROUP_KEY_CANNOT_HAVE_MEMBERS", "group API keys cannot include aggregate members")
)

// AggregateModelSupportChecker decides whether a member group can serve a requested model.
type AggregateModelSupportChecker interface {
	GroupSupportsRequestedModel(ctx context.Context, group *Group, requestedModel string) bool
}

func NormalizeAPIKeyKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case APIKeyKindAggregate:
		return APIKeyKindAggregate
	default:
		return APIKeyKindGroup
	}
}

func (k *APIKey) IsAggregate() bool {
	return k != nil && NormalizeAPIKeyKind(k.KeyKind) == APIKeyKindAggregate
}

func (k *APIKey) IsMemberUsable() bool {
	if k == nil || !k.IsActive() || k.IsExpired() || k.IsQuotaExhausted() {
		return false
	}
	if k.IsAggregate() || k.GroupID == nil || k.Group == nil || !k.Group.IsActive() {
		return false
	}
	return true
}

func ValidateAggregateMembers(userID int64, members []*APIKey) error {
	if len(members) == 0 {
		return ErrAggregateMembersRequired
	}
	if len(members) > maxAggregateMembers {
		return ErrAggregateTooManyMembers
	}
	seenGroups := make(map[int64]struct{}, len(members))
	seenKeys := make(map[int64]struct{}, len(members))
	for _, member := range members {
		if member == nil {
			return ErrAggregateMemberNotFound
		}
		if _, dup := seenKeys[member.ID]; dup {
			return ErrAggregateDuplicateGroup
		}
		seenKeys[member.ID] = struct{}{}
		if member.UserID != userID {
			return ErrAggregateMemberNotOwned
		}
		if member.IsAggregate() {
			return ErrAggregateNested
		}
		if member.GroupID == nil || *member.GroupID <= 0 {
			return ErrAggregateMemberNoGroup
		}
		if _, dup := seenGroups[*member.GroupID]; dup {
			return ErrAggregateDuplicateGroup
		}
		seenGroups[*member.GroupID] = struct{}{}
	}
	return nil
}

func ResolveAggregateMember(ctx context.Context, aggregate *APIKey, requestedModel string, checker AggregateModelSupportChecker) (*APIKey, error) {
	if aggregate == nil || !aggregate.IsAggregate() {
		return aggregate, nil
	}
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		return nil, ErrAggregateModelRequired
	}
	if checker == nil {
		return nil, ErrAggregateNoMatchingMember
	}
	for i := range aggregate.Members {
		member := aggregate.Members[i]
		if member == nil || !member.IsMemberUsable() {
			continue
		}
		if checker.GroupSupportsRequestedModel(ctx, member.Group, model) {
			return member, nil
		}
	}
	return nil, ErrAggregateNoMatchingMember
}

func BindAggregateRoute(aggregate *APIKey, member *APIKey) *APIKey {
	if aggregate == nil || member == nil {
		return aggregate
	}
	routed := *aggregate
	routed.GroupID = member.GroupID
	routed.Group = member.Group
	routed.RoutedMemberKeyID = member.ID
	routed.RoutedMember = member
	return &routed
}

func (k *APIKey) MemberIDs() []int64 {
	if k == nil || len(k.Members) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(k.Members))
	for _, member := range k.Members {
		if member == nil || member.ID <= 0 {
			continue
		}
		ids = append(ids, member.ID)
	}
	return ids
}

type apiKeyAggregateStore interface {
	SetKeyKind(ctx context.Context, id int64, kind string) error
	ReplaceAggregateMembers(ctx context.Context, aggregateID int64, memberIDs []int64) error
	DeleteAggregateMembershipsForKey(ctx context.Context, keyID int64) error
	ListAggregateKeysByMemberID(ctx context.Context, memberID int64) ([]string, error)
}

func (s *APIKeyService) persistAggregateMetadata(ctx context.Context, key *APIKey, memberIDs []int64) error {
	store, ok := s.apiKeyRepo.(apiKeyAggregateStore)
	if !ok {
		if key.IsAggregate() {
			return fmt.Errorf("aggregate API keys are not supported by this repository")
		}
		return nil
	}
	if err := store.SetKeyKind(ctx, key.ID, NormalizeAPIKeyKind(key.KeyKind)); err != nil {
		return err
	}
	if !key.IsAggregate() {
		return nil
	}
	return store.ReplaceAggregateMembers(ctx, key.ID, memberIDs)
}

func (s *APIKeyService) rollbackCreatedAPIKey(ctx context.Context, id int64) {
	if s == nil || id <= 0 {
		return
	}
	if store, ok := s.apiKeyRepo.(apiKeyAggregateStore); ok {
		_ = store.DeleteAggregateMembershipsForKey(ctx, id)
	}
	_ = s.apiKeyRepo.DeleteWithAudit(ctx, id)
}

func (s *APIKeyService) loadAggregateMembers(ctx context.Context, userID int64, memberIDs []int64) ([]*APIKey, error) {
	members := make([]*APIKey, 0, len(memberIDs))
	for _, id := range memberIDs {
		member, err := s.apiKeyRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, ErrAPIKeyNotFound) {
				return nil, ErrAggregateMemberNotFound
			}
			return nil, err
		}
		members = append(members, member)
	}
	if err := ValidateAggregateMembers(userID, members); err != nil {
		return nil, err
	}
	return members, nil
}

func (s *APIKeyService) invalidateAggregateParents(ctx context.Context, memberID int64) {
	if s == nil || memberID <= 0 {
		return
	}
	store, ok := s.apiKeyRepo.(apiKeyAggregateStore)
	if !ok {
		return
	}
	keys, err := store.ListAggregateKeysByMemberID(ctx, memberID)
	if err != nil || len(keys) == 0 {
		return
	}
	for _, key := range keys {
		s.InvalidateAuthCacheByKey(ctx, key)
	}
}
