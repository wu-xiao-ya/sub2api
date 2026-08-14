//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type runtimeBlockRecorder struct {
	accounts   []*Account
	until      []time.Time
	reasons    []string
	clearedIDs []int64
}

func (r *runtimeBlockRecorder) BlockAccountScheduling(account *Account, until time.Time, reason string) {
	r.accounts = append(r.accounts, account)
	r.until = append(r.until, until)
	r.reasons = append(r.reasons, reason)
}

func (r *runtimeBlockRecorder) ClearAccountSchedulingBlock(accountID int64) {
	r.clearedIDs = append(r.clearedIDs, accountID)
}

func TestRateLimitService_HandleUpstreamError_OpenAI403FirstHitTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{1}}
	blocker := &runtimeBlockRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	service.SetAccountRuntimeBlocker(blocker)
	account := &Account{
		ID:       301,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary edge rejection"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "temporary edge rejection")
	require.Contains(t, repo.lastTempReason, "(1/3)")
	require.Len(t, blocker.accounts, 1)
	require.Equal(t, account.ID, blocker.accounts[0].ID)
	require.Equal(t, "openai_403_temp", blocker.reasons[0])
	require.True(t, blocker.until[0].After(time.Now()))
}

func TestRateLimitService_HandleUpstreamError_OpenAI403ThresholdDisables(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{3}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       302,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"workspace forbidden by policy"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Contains(t, repo.lastErrorMsg, "workspace forbidden by policy")
	require.Contains(t, repo.lastErrorMsg, "consecutive_403=3/3")
}

func TestRateLimitService_HandleUpstreamError_OpenAIGenericBlocked403UsesModelCooldown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	counter := &openAI403CounterCacheStub{
		modelDistinct: []int64{1},
		modelAdded:    []bool{true},
	}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       303,
		Name:     "plus-gateway-1",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	shouldFailover := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{"X-Request-Id": []string{"req-model-403"}},
		[]byte(`Your request was blocked.`),
		"gpt-5.6-terra",
	)

	require.True(t, shouldFailover)
	require.Equal(t, []string{"gpt-5.6-terra"}, counter.modelRecords)
	require.Empty(t, counter.counts)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, account.ID, repo.modelRateLimitCalls[0].accountID)
	require.Equal(t, "gpt-5.6-terra", repo.modelRateLimitCalls[0].scope)
	require.Contains(t, repo.modelRateLimitCalls[0].reason, "Your request was blocked")
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
}

func TestRateLimitService_HandleUpstreamError_OpenAIGenericBlocked403SecondModelEscalatesAccount(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{
		counts:        []int64{1},
		modelDistinct: []int64{2},
		modelAdded:    []bool{true},
	}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       304,
		Name:     "plus-gateway-2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldFailover := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Your request was blocked."}}`),
		"gpt-5.5",
	)

	require.True(t, shouldFailover)
	require.Equal(t, []string{"gpt-5.5"}, counter.modelRecords)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "(1/3)")
}

func TestRateLimitService_HandleUpstreamError_OpenAIExplicitAuth403StaysAccountScoped(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{1}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       305,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldFailover := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Your request was blocked because the token was revoked."}}`),
		"gpt-5.6-terra",
	)

	require.True(t, shouldFailover)
	require.Empty(t, counter.modelRecords)
	require.Equal(t, 1, repo.tempCalls)
}

func TestOpenAIGenericBlocked403Classifier(t *testing.T) {
	require.True(t, isOpenAIGenericBlocked403("", []byte(`Your request was blocked.`)))
	require.True(t, isOpenAIGenericBlocked403("YOUR REQUEST WAS BLOCKED", nil))
	require.False(t, isOpenAIGenericBlocked403("", []byte(`workspace forbidden by policy`)))

	require.True(t, isExplicitOpenAI403AuthFailure("", []byte(`Your request was blocked: invalid_api_key`)))
	require.False(t, isExplicitOpenAI403AuthFailure("", []byte(`Your request was blocked.`)))
}
