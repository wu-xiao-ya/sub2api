package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type aggregateHandlerModelChecker struct{ handler *GatewayHandler }

func (c aggregateHandlerModelChecker) GroupSupportsRequestedModel(ctx context.Context, group *service.Group, model string) bool {
	return c.handler.AggregateGroupSupportsModel(ctx, group, model)
}

func TestAggregateSkipsUnavailableGroupsInsteadOfAdvertisingDefaultModels(t *testing.T) {
	ctx := context.Background()
	emptyID, disabledID, limitedID, healthyID := int64(1), int64(2), int64(3), int64(4)
	future := time.Now().Add(time.Hour)
	account := service.Account{ID: 10, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}}
	disabled := account
	disabled.Schedulable = false
	limited := account
	limited.RateLimitResetAt = &future
	repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{emptyID: nil, disabledID: {disabled}, limitedID: {limited}, healthyID: {account}}}
	h := newGatewayModelsHandlerForTest(repo)
	root := &service.APIKey{ID: 100, UserID: 7, KeyKind: service.APIKeyKindAggregate}
	for _, id := range []int64{emptyID, disabledID, limitedID, healthyID} {
		groupID := id
		root.Members = append(root.Members, &service.APIKey{ID: id, UserID: 7, Status: service.StatusActive, GroupID: &groupID, Group: &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive}})
	}
	for _, member := range root.Members[:3] {
		require.False(t, h.AggregateGroupSupportsModel(ctx, member.Group, "gpt-6.1-sol"))
		require.Empty(t, h.aggregateGroupModels(ctx, member.Group))
	}
	member, err := service.ResolveAggregateMember(ctx, root, "gpt-6.1-sol", aggregateHandlerModelChecker{h})
	require.NoError(t, err)
	require.Equal(t, healthyID, member.ID)
	require.Equal(t, []string{"gpt-6.1-sol"}, h.aggregateModels(ctx, root))
	// A cached catalog cannot keep a group eligible after its capacity disappears.
	repo.byGroup[healthyID] = nil
	require.False(t, h.AggregateGroupSupportsModel(ctx, root.Members[3].Group, "gpt-6.1-sol"))
	require.Empty(t, h.aggregateModels(ctx, root))
}

func TestAggregateStillUsesDefaultsForHealthyUnmappedAccounts(t *testing.T) {
	ctx := context.Background()
	id := int64(5)
	repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {{ID: 11, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true}}}}
	h := newGatewayModelsHandlerForTest(repo)
	group := &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive}
	require.True(t, h.AggregateGroupSupportsModel(ctx, group, "gpt-6.1-sol"))
}
