//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type accountProbeRepoStub struct {
	calls         []accountProbeRepoCall
	accounts      []Account
	err           error
	groupPlatform string
}

type accountProbeRepoCall struct {
	groupName string
	platform  string
}

func (r *accountProbeRepoStub) ListSchedulableByGroupNameAndPlatform(
	_ context.Context,
	groupName, platform string,
) ([]Account, error) {
	r.calls = append(r.calls, accountProbeRepoCall{groupName: groupName, platform: platform})
	if r.err != nil {
		return nil, r.err
	}
	return append([]Account(nil), r.accounts...), nil
}

func (r *accountProbeRepoStub) ListSchedulableByGroupIDAndPlatform(
	_ context.Context,
	_ int64,
	platform string,
) ([]Account, error) {
	r.calls = append(r.calls, accountProbeRepoCall{platform: platform})
	if r.err != nil {
		return nil, r.err
	}
	return append([]Account(nil), r.accounts...), nil
}

func (r *accountProbeRepoStub) GetGroupPlatform(_ context.Context, _ int64) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.groupPlatform != "" {
		return r.groupPlatform, nil
	}
	return PlatformOpenAI, nil
}

type accountProbeHTTPStub struct {
	mu        sync.Mutex
	requests  map[int64]accountProbeRequestSnapshot
	order     []int64
	delays    map[int64]time.Duration
	texts     map[int64]string
	statuses  map[int64]int
	started   chan int64
	release   <-chan struct{}
	doWithTLS int
}

type accountProbeRequestSnapshot struct {
	path      string
	auth      string
	body      map[string]any
	proxyURL  string
	accountID int64
}

func (u *accountProbeHTTPStub) Do(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *accountProbeHTTPStub) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	_ int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	if u.started != nil {
		u.started <- accountID
	}
	if u.release != nil {
		<-u.release
	}
	if delay := u.delays[accountID]; delay > 0 {
		time.Sleep(delay)
	}

	rawBody, _ := io.ReadAll(req.Body)
	body := map[string]any{}
	_ = json.Unmarshal(rawBody, &body)

	u.mu.Lock()
	if u.requests == nil {
		u.requests = map[int64]accountProbeRequestSnapshot{}
	}
	u.doWithTLS++
	u.order = append(u.order, accountID)
	u.requests[accountID] = accountProbeRequestSnapshot{
		path:      req.URL.Path,
		auth:      req.Header.Get("Authorization"),
		body:      body,
		proxyURL:  proxyURL,
		accountID: accountID,
	}
	u.mu.Unlock()

	status := u.statuses[accountID]
	if status == 0 {
		status = http.StatusOK
	}
	text := u.texts[accountID]
	if text == "" {
		text = "1"
	}
	responseBody := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, text)
	if strings.HasSuffix(req.URL.Path, "/responses") {
		responseBody = fmt.Sprintf(`{"output_text":%q}`, text)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}, nil
}

func newAccountProbeTestService(
	monitorRepo *groupProbeRepoStub,
	accountRepo *accountProbeRepoStub,
	upstream *accountProbeHTTPStub,
) *ChannelMonitorService {
	svc := NewChannelMonitorService(monitorRepo, groupProbeEncryptor{})
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)
	return svc
}

func TestRunCheckBusinessGroupProbesAccountsAndPersistsBest(t *testing.T) {
	monitorRepo := &groupProbeRepoStub{
		monitors: map[int64]*ChannelMonitor{
			10: {
				ID:           10,
				Name:         "plus-sol",
				Provider:     MonitorProviderOpenAI,
				APIMode:      MonitorAPIModeChatCompletions,
				Endpoint:     "https://monitor.example.com",
				APIKey:       "sk-monitor",
				PrimaryModel: "gpt-5.6-sol",
				GroupName:    "gpt-plus",
				Enabled:      true,
			},
		},
	}
	accountRepo := &accountProbeRepoStub{accounts: []Account{
		openAIProbeAccount(1, "slow", 20, nil),
		openAIProbeAccount(2, "fast", 10, map[string]any{"gpt-5.6-sol": "upstream-sol-fast"}),
		openAIProbeAccount(3, "bad", 30, nil),
	}}
	upstream := &accountProbeHTTPStub{
		delays: map[int64]time.Duration{
			1: 30 * time.Millisecond,
			2: time.Millisecond,
			3: 2 * time.Millisecond,
		},
		texts: map[int64]string{3: "0"},
	}

	results, err := newAccountProbeTestService(monitorRepo, accountRepo, upstream).RunCheck(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunCheck returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d result(s), want 1 best account result", len(results))
	}
	got := results[0]
	if got.Status != MonitorStatusOperational {
		t.Fatalf("best result status = %s message=%q, want operational", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, `account group "gpt-plus" probed 3 accounts, 2 healthy; best account #2 fast`) {
		t.Fatalf("best result should include account group summary, got %q", got.Message)
	}
	if got.AccountID == nil || *got.AccountID != 2 {
		t.Fatalf("best result account id = %v, want 2", got.AccountID)
	}
	if got.AccountName != "fast" {
		t.Fatalf("best result account name = %q, want fast", got.AccountName)
	}
	if got.ProbeMode != accountProbeModeFull {
		t.Fatalf("best result probe mode = %q, want %q", got.ProbeMode, accountProbeModeFull)
	}
	if got.CandidateCount != 3 || got.HealthyCount != 2 {
		t.Fatalf("best result candidate summary = %d/%d, want 3/2", got.CandidateCount, got.HealthyCount)
	}
	if len(accountRepo.calls) != 1 || accountRepo.calls[0].groupName != "gpt-plus" || accountRepo.calls[0].platform != PlatformOpenAI {
		t.Fatalf("unexpected account repo calls: %#v", accountRepo.calls)
	}
	if len(monitorRepo.rows) != 1 || len(monitorRepo.marked) != 1 {
		t.Fatalf("expected one persisted best result, rows=%d marked=%d", len(monitorRepo.rows), len(monitorRepo.marked))
	}
	if monitorRepo.rows[0].MonitorID != 10 || monitorRepo.rows[0].Model != "gpt-5.6-sol" || monitorRepo.rows[0].Status != MonitorStatusOperational {
		t.Fatalf("unexpected persisted row: %#v", monitorRepo.rows[0])
	}
	if monitorRepo.rows[0].AccountID == nil || *monitorRepo.rows[0].AccountID != 2 {
		t.Fatalf("persisted account id = %v, want 2", monitorRepo.rows[0].AccountID)
	}
	if monitorRepo.rows[0].AccountName != "fast" ||
		monitorRepo.rows[0].ProbeMode != accountProbeModeFull ||
		monitorRepo.rows[0].CandidateCount != 3 ||
		monitorRepo.rows[0].HealthyCount != 2 {
		t.Fatalf("persisted probe metadata mismatch: %#v", monitorRepo.rows[0])
	}

	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.doWithTLS != 3 {
		t.Fatalf("DoWithTLS calls = %d, want 3", upstream.doWithTLS)
	}
	fastReq := upstream.requests[2]
	if fastReq.path != "/v1/chat/completions" {
		t.Fatalf("fast account path = %q, want /v1/chat/completions", fastReq.path)
	}
	if fastReq.auth != "Bearer sk-fast" {
		t.Fatalf("fast account auth = %q, want account API key", fastReq.auth)
	}
	if fastReq.body["model"] != "upstream-sol-fast" {
		t.Fatalf("fast account mapped model = %#v, want upstream-sol-fast", fastReq.body["model"])
	}
	if fastReq.body["max_tokens"] != float64(monitorLowCostMaxTokens) {
		t.Fatalf("max_tokens = %#v, want %d", fastReq.body["max_tokens"], monitorLowCostMaxTokens)
	}
}

func TestRunCheckBusinessGroupLimitsCandidatesAndRunsFiveConcurrently(t *testing.T) {
	monitorRepo := &groupProbeRepoStub{
		monitors: map[int64]*ChannelMonitor{
			20: {
				ID:           20,
				Name:         "plus-sol",
				Provider:     MonitorProviderOpenAI,
				APIMode:      MonitorAPIModeChatCompletions,
				Endpoint:     "https://monitor.example.com",
				APIKey:       "sk-monitor",
				PrimaryModel: "gpt-5.6-sol",
				GroupName:    "gpt-plus",
				Enabled:      true,
			},
		},
	}
	accounts := make([]Account, 0, monitorGroupProbeMaxCandidates+2)
	for i := 1; i <= monitorGroupProbeMaxCandidates+2; i++ {
		accounts = append(accounts, openAIProbeAccount(int64(i), fmt.Sprintf("line-%d", i), i, nil))
	}
	accountRepo := &accountProbeRepoStub{accounts: accounts}
	started := make(chan int64, monitorGroupProbeMaxCandidates+2)
	release := make(chan struct{})
	upstream := &accountProbeHTTPStub{started: started, release: release}
	svc := newAccountProbeTestService(monitorRepo, accountRepo, upstream)

	done := make(chan error, 1)
	go func() {
		_, err := svc.RunCheck(context.Background(), 20)
		done <- err
	}()

	for i := 0; i < monitorGroupProbeMaxCandidates; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("candidate %d did not start before earlier requests completed", i+1)
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunCheck returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunCheck did not complete")
	}

	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.doWithTLS != monitorGroupProbeMaxCandidates {
		t.Fatalf("DoWithTLS calls = %d, want capped %d", upstream.doWithTLS, monitorGroupProbeMaxCandidates)
	}
	for _, id := range upstream.order {
		if id > monitorGroupProbeMaxCandidates {
			t.Fatalf("unexpected uncapped account probe for id=%d, order=%v", id, upstream.order)
		}
	}
}

func TestResolveBusinessGroupProbeSkipsImageMode(t *testing.T) {
	accountRepo := &accountProbeRepoStub{accounts: []Account{openAIProbeAccount(1, "line-1", 1, nil)}}
	upstream := &accountProbeHTTPStub{}
	svc := newAccountProbeTestService(&groupProbeRepoStub{}, accountRepo, upstream)

	_, _, matched, err := svc.resolveBusinessGroupProbeAccounts(context.Background(), &ChannelMonitor{
		Provider:  MonitorProviderOpenAI,
		APIMode:   MonitorAPIModeImages,
		GroupName: "gpt-plus",
	})
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}
	if matched {
		t.Fatal("image mode must not use account group probing")
	}
	if len(accountRepo.calls) != 0 {
		t.Fatalf("image mode should not query account groups, calls=%#v", accountRepo.calls)
	}
}

func openAIProbeAccount(id int64, name string, priority int, modelMapping map[string]any) Account {
	credentials := map[string]any{
		"api_key":  fmt.Sprintf("sk-%s", name),
		"base_url": "https://127.0.0.1:1/v1",
	}
	if modelMapping != nil {
		credentials["model_mapping"] = modelMapping
	}
	return Account{
		ID:          id,
		Name:        name,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: credentials,
		Concurrency: 10,
		Priority:    priority,
		Status:      StatusActive,
		Schedulable: true,
	}
}
