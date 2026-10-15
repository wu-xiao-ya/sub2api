//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type probeOnlyAccountRepoSpy struct {
	AccountRepository
	account *Account
	writes  []string
}

func (r *probeOnlyAccountRepoSpy) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *probeOnlyAccountRepoSpy) record(name string) {
	r.writes = append(r.writes, name)
}

func (r *probeOnlyAccountRepoSpy) Update(context.Context, *Account) error {
	r.record("update")
	return nil
}

func (r *probeOnlyAccountRepoSpy) SetError(context.Context, int64, string) error {
	r.record("set_error")
	return nil
}

func (r *probeOnlyAccountRepoSpy) SetRateLimited(context.Context, int64, time.Time) error {
	r.record("set_rate_limited")
	return nil
}

func (r *probeOnlyAccountRepoSpy) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	r.record("set_model_rate_limit")
	return nil
}

func (r *probeOnlyAccountRepoSpy) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.record("set_temp_unschedulable")
	return nil
}

func (r *probeOnlyAccountRepoSpy) UpdateExtra(context.Context, int64, map[string]any) error {
	r.record("update_extra")
	return nil
}

type probeOnlyAntigravityUpstream struct {
	calls int
}

func (u *probeOnlyAntigravityUpstream) Do(
	*http.Request,
	string,
	int64,
	int,
) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"status": "RESOURCE_EXHAUSTED",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {"model": "gemini-2.5-flash"},
						"reason": "RATE_LIMIT_EXCEEDED"
					},
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": "30s"
					}
				]
			}
		}`)),
	}, nil
}

func (u *probeOnlyAntigravityUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestAccountMonitorProbeAntigravityDoesNotMutateAccountState(t *testing.T) {
	account := &Account{
		ID:          71,
		Name:        "antigravity-probe",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "access-token",
			"project_id":   "probe-project",
			"model_mapping": map[string]any{
				"gemini-2.5-flash": "gemini-2.5-flash",
			},
		},
		Extra: map[string]any{
			"allow_overages": true,
		},
	}
	repo := &probeOnlyAccountRepoSpy{account: account}
	upstream := &probeOnlyAntigravityUpstream{}
	gateway := &AntigravityGatewayService{
		accountRepo:   repo,
		tokenProvider: &AntigravityTokenProvider{accountRepo: repo},
		httpUpstream:  upstream,
	}
	service := &AccountTestService{
		accountRepo:               repo,
		antigravityGatewayService: gateway,
		httpUpstream:              upstream,
	}

	result, err := service.ProbeForChannelMonitor(context.Background(), account.ID, "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("ProbeForChannelMonitor returned error: %v", err)
	}
	if result.Success {
		t.Fatalf("probe unexpectedly succeeded: %#v", result)
	}
	if upstream.calls != 1 {
		t.Fatalf("upstream calls = %d, want one low-cost request without retry fan-out", upstream.calls)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("probe wrote production account state: %#v", repo.writes)
	}
	if !account.IsOveragesEnabled() {
		t.Fatal("probe modified the original account overages setting")
	}
}
