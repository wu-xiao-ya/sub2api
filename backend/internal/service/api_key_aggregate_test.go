package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAPIKeyKind(t *testing.T) {
	require.Equal(t, APIKeyKindGroup, NormalizeAPIKeyKind(""))
	require.Equal(t, APIKeyKindGroup, NormalizeAPIKeyKind("group"))
	require.Equal(t, APIKeyKindAggregate, NormalizeAPIKeyKind("AGGREGATE"))
}

func TestValidateAggregateMembers(t *testing.T) {
	gid1, gid2 := int64(11), int64(12)
	openai := &APIKey{ID: 1, UserID: 7, KeyKind: APIKeyKindGroup, GroupID: &gid1, Group: &Group{ID: gid1, Status: StatusActive, Platform: PlatformOpenAI}}
	claude := &APIKey{ID: 2, UserID: 7, KeyKind: APIKeyKindGroup, GroupID: &gid2, Group: &Group{ID: gid2, Status: StatusActive, Platform: PlatformAnthropic}}

	require.ErrorIs(t, ValidateAggregateMembers(7, nil), ErrAggregateMembersRequired)
	require.ErrorIs(t, ValidateAggregateMembers(8, []*APIKey{openai}), ErrAggregateMemberNotOwned)
	require.ErrorIs(t, ValidateAggregateMembers(7, []*APIKey{openai, {ID: 3, UserID: 7, KeyKind: APIKeyKindAggregate, GroupID: &gid2}}), ErrAggregateNested)
	require.ErrorIs(t, ValidateAggregateMembers(7, []*APIKey{openai, {ID: 4, UserID: 7, KeyKind: APIKeyKindGroup, GroupID: &gid1}}), ErrAggregateDuplicateGroup)
	require.NoError(t, ValidateAggregateMembers(7, []*APIKey{openai, claude}))
}

type staticModelChecker struct {
	supported map[int64][]string
}

func (c staticModelChecker) GroupSupportsRequestedModel(_ context.Context, group *Group, requestedModel string) bool {
	if group == nil {
		return false
	}
	for _, model := range c.supported[group.ID] {
		if model == requestedModel {
			return true
		}
	}
	return false
}

func TestResolveAggregateMemberSkipsUnusableAndUsesOrder(t *testing.T) {
	gid1, gid2, gid3 := int64(21), int64(22), int64(23)
	disabled := &APIKey{ID: 1, UserID: 9, Status: StatusAPIKeyDisabled, KeyKind: APIKeyKindGroup, GroupID: &gid1, Group: &Group{ID: gid1, Status: StatusActive}}
	claude := &APIKey{ID: 3, UserID: 9, Status: StatusActive, KeyKind: APIKeyKindGroup, GroupID: &gid2, Group: &Group{ID: gid2, Status: StatusActive, Platform: PlatformAnthropic}}
	openai := &APIKey{ID: 4, UserID: 9, Status: StatusActive, KeyKind: APIKeyKindGroup, GroupID: &gid3, Group: &Group{ID: gid3, Status: StatusActive, Platform: PlatformOpenAI}}
	aggregate := &APIKey{
		ID:      99,
		UserID:  9,
		Status:  StatusActive,
		KeyKind: APIKeyKindAggregate,
		Members: []*APIKey{disabled, claude, openai},
	}
	checker := staticModelChecker{supported: map[int64][]string{
		gid2: {"claude-sonnet-4-5"},
		gid3: {"gpt-5.4", "claude-sonnet-4-5"},
	}}

	member, err := ResolveAggregateMember(context.Background(), aggregate, "gpt-5.4", checker)
	require.NoError(t, err)
	require.Equal(t, int64(4), member.ID)

	member, err = ResolveAggregateMember(context.Background(), aggregate, "claude-sonnet-4-5", checker)
	require.NoError(t, err)
	require.Equal(t, int64(3), member.ID)

	_, err = ResolveAggregateMember(context.Background(), aggregate, "gemini-3.1-pro", checker)
	require.ErrorIs(t, err, ErrAggregateNoMatchingMember)

	routed := BindAggregateRoute(aggregate, member)
	require.Equal(t, int64(99), routed.ID)
	require.Equal(t, int64(3), routed.RoutedMemberKeyID)
	require.NotNil(t, routed.GroupID)
	require.Equal(t, gid2, *routed.GroupID)
}
