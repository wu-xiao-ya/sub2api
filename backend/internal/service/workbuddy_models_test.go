package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkBuddySelectionControlsSchedulingAndPublishedModels(t *testing.T) {
	account := Account{ID: 1, Platform: PlatformDeepseek, Type: AccountTypePlugin}
	require.True(t, account.IsModelSupported("deepseek-v4-flash"))
	require.False(t, account.IsModelSupported("qwen3"))
	account.Extra = map[string]any{WorkBuddyModelsExtraKey: []any{"qwen3", "deepseek-v4-flash"}}
	require.True(t, account.IsModelSupported("qwen3"))
	require.False(t, account.IsModelSupported("deepseek-v3-2-volc"))

	group := int64(90)
	repo := &modelsListAccountRepoStub{byGroup: map[int64][]Account{group: {account}}}
	gateway := &GatewayService{accountRepo: repo}
	require.Equal(t, []string{"deepseek-v4-flash", "qwen3"}, gateway.GetAvailableModels(context.Background(), &group, PlatformDeepseek))
	account.Extra = map[string]any{WorkBuddyModelsExtraKey: []any{}}
	repo.byGroup[group] = []Account{account}
	require.False(t, account.IsModelSupported("deepseek-v4-flash"))
	require.Equal(t, []string{}, gateway.GetAvailableModels(context.Background(), &group, PlatformDeepseek))
	_, err := (&OpenAIGatewayService{}).sendCCUpstreamRequest(context.Background(), nil, &account,
		pluginProviderBaseURL, []byte(`{"model":"deepseek-v4-flash"}`), true, "", "", "")
	require.ErrorContains(t, err, "disabled for this WorkBuddy account")
}
