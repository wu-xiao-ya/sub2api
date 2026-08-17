//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
)

type adaptiveProbeCall struct {
	accountID int64
	model     string
}

type adaptiveProbeExecutorStub struct {
	mu        sync.Mutex
	results   map[int64][]AccountMonitorProbeResult
	callIndex map[int64]int
	calls     []adaptiveProbeCall
}

func (s *adaptiveProbeExecutorStub) ProbeForChannelMonitor(
	_ context.Context,
	accountID int64,
	model string,
) (*AccountMonitorProbeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, adaptiveProbeCall{accountID: accountID, model: model})
	index := s.callIndex[accountID]
	s.callIndex[accountID] = index + 1
	results := s.results[accountID]
	if index >= len(results) {
		return &AccountMonitorProbeResult{Success: false, LatencyMs: 1, Message: "missing test result"}, nil
	}
	result := results[index]
	return &result, nil
}

func (s *adaptiveProbeExecutorStub) count(accountID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, call := range s.calls {
		if call.accountID == accountID {
			count++
		}
	}
	return count
}

type adaptiveSettingsProvider struct {
	settings ChannelMonitorAccountProbeSettings
}

func (s adaptiveSettingsProvider) GetChannelMonitorAccountProbeSettings(context.Context) ChannelMonitorAccountProbeSettings {
	return s.settings
}

type anthropicAdaptiveRequestSnapshot struct {
	url         string
	headers     http.Header
	body        map[string]any
	proxyURL    string
	accountID   int64
	concurrency int
}

type anthropicAdaptiveHTTPStub struct {
	mu           sync.Mutex
	status       int
	responseBody string
	requests     []anthropicAdaptiveRequestSnapshot
}

type geminiAdaptiveHTTPStub struct {
	mu           sync.Mutex
	status       int
	responseBody string
	requests     []anthropicAdaptiveRequestSnapshot
}

func (s *geminiAdaptiveHTTPStub) Do(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
) (*http.Response, error) {
	return s.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (s *geminiAdaptiveHTTPStub) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	rawBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.requests = append(s.requests, anthropicAdaptiveRequestSnapshot{
		url:         req.URL.String(),
		headers:     req.Header.Clone(),
		body:        body,
		proxyURL:    proxyURL,
		accountID:   accountID,
		concurrency: accountConcurrency,
	})
	s.mu.Unlock()

	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	responseBody := s.responseBody
	if responseBody == "" {
		responseBody = `{"candidates":[{"content":{"parts":[{"text":"1"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1,"cachedContentTokenCount":1}}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}, nil
}

func (s *geminiAdaptiveHTTPStub) latestRequest(t *testing.T) anthropicAdaptiveRequestSnapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("expected a Gemini adaptive probe request")
	}
	return s.requests[len(s.requests)-1]
}

func (s *anthropicAdaptiveHTTPStub) Do(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
) (*http.Response, error) {
	return s.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (s *anthropicAdaptiveHTTPStub) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	rawBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.requests = append(s.requests, anthropicAdaptiveRequestSnapshot{
		url:         req.URL.String(),
		headers:     req.Header.Clone(),
		body:        body,
		proxyURL:    proxyURL,
		accountID:   accountID,
		concurrency: accountConcurrency,
	})
	s.mu.Unlock()

	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	responseBody := s.responseBody
	if responseBody == "" {
		responseBody = `{"content":[{"type":"text","text":"1"}],"usage":{"input_tokens":3,"output_tokens":1}}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}, nil
}

func (s *anthropicAdaptiveHTTPStub) latestRequest(t *testing.T) anthropicAdaptiveRequestSnapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("expected an Anthropic adaptive probe request")
	}
	return s.requests[len(s.requests)-1]
}

func adaptiveHeaderValue(headers http.Header, name string) string {
	for existing, values := range headers {
		if strings.EqualFold(existing, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

type adaptiveMonitorRepoStub struct {
	*groupProbeRepoStub
	mu          sync.Mutex
	states      map[string]*ChannelMonitorAccountProbeState
	latestImage *ChannelMonitorLatestImage
}

func adaptiveStateKey(monitorID int64, model string) string {
	return string(rune(monitorID)) + "\x00" + model
}

func (r *adaptiveMonitorRepoStub) GetAccountProbeState(
	_ context.Context,
	monitorID int64,
	model string,
) (*ChannelMonitorAccountProbeState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[adaptiveStateKey(monitorID, model)]
	if state == nil {
		return nil, nil
	}
	copy := *state
	if state.AccountID != nil {
		id := *state.AccountID
		copy.AccountID = &id
	}
	if state.LastLatencyMs != nil {
		latency := *state.LastLatencyMs
		copy.LastLatencyMs = &latency
	}
	if state.LastFullSweepAt != nil {
		fullSweepAt := *state.LastFullSweepAt
		copy.LastFullSweepAt = &fullSweepAt
	}
	return &copy, nil
}

func (r *adaptiveMonitorRepoStub) UpsertAccountProbeState(
	_ context.Context,
	state *ChannelMonitorAccountProbeState,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *state
	if state.AccountID != nil {
		id := *state.AccountID
		copy.AccountID = &id
	}
	if state.LastLatencyMs != nil {
		latency := *state.LastLatencyMs
		copy.LastLatencyMs = &latency
	}
	if state.LastFullSweepAt == nil {
		if previous := r.states[adaptiveStateKey(state.MonitorID, state.Model)]; previous != nil &&
			previous.LastFullSweepAt != nil {
			fullSweepAt := *previous.LastFullSweepAt
			copy.LastFullSweepAt = &fullSweepAt
		}
	} else {
		fullSweepAt := *state.LastFullSweepAt
		copy.LastFullSweepAt = &fullSweepAt
	}
	r.states[adaptiveStateKey(state.MonitorID, state.Model)] = &copy
	return nil
}

func (r *adaptiveMonitorRepoStub) ClearAccountProbeStates(_ context.Context, monitorID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, state := range r.states {
		if state.MonitorID == monitorID {
			delete(r.states, key)
		}
	}
	return nil
}

func (r *adaptiveMonitorRepoStub) UpsertLatestImage(_ context.Context, image *ChannelMonitorLatestImage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *image
	copy.Data = append([]byte(nil), image.Data...)
	r.latestImage = &copy
	return nil
}

func (r *adaptiveMonitorRepoStub) GetLatestImage(_ context.Context, monitorID int64) (*ChannelMonitorLatestImage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latestImage == nil || r.latestImage.MonitorID != monitorID {
		return nil, ErrChannelMonitorLatestImageNotFound
	}
	copy := *r.latestImage
	copy.Data = append([]byte(nil), r.latestImage.Data...)
	return &copy, nil
}

func newAdaptiveMonitorTestService(
	t *testing.T,
	state *ChannelMonitorAccountProbeState,
	executor *adaptiveProbeExecutorStub,
) (*ChannelMonitorService, *adaptiveMonitorRepoStub) {
	t.Helper()
	groupID := int64(7)
	monitor := &ChannelMonitor{
		ID:             11,
		Name:           "adaptive-plus",
		Provider:       MonitorProviderOpenAI,
		APIMode:        MonitorAPIModeChatCompletions,
		Endpoint:       "https://monitor.example.com",
		APIKey:         "sk-static-fallback",
		PrimaryModel:   "gpt-test",
		AccountGroupID: &groupID,
		Enabled:        true,
	}
	repo := &adaptiveMonitorRepoStub{
		groupProbeRepoStub: &groupProbeRepoStub{
			monitors: map[int64]*ChannelMonitor{monitor.ID: monitor},
		},
		states: map[string]*ChannelMonitorAccountProbeState{},
	}
	if state != nil {
		repo.states[adaptiveStateKey(state.MonitorID, state.Model)] = state
	}
	accounts := &accountProbeRepoStub{accounts: []Account{
		openAIProbeAccount(1, "line-1", 1, nil),
		openAIProbeAccount(2, "line-2", 2, nil),
	}}
	svc := NewChannelMonitorService(repo, groupProbeEncryptor{})
	svc.SetAccountProbeDependencies(accounts, nil, nil, nil)
	svc.SetAccountProbeExecutor(executor)
	svc.SetAccountProbeSettingsProvider(adaptiveSettingsProvider{
		settings: ChannelMonitorAccountProbeSettings{
			Enabled:             true,
			ConfirmAttempts:     1,
			DegradedThresholdMs: 6000,
			MaxCandidates:       5,
			Parallelism:         5,
		},
	})
	return svc, repo
}

func TestAdaptiveAccountProbeUsesStickyAccountWhenHealthy(t *testing.T) {
	accountID := int64(1)
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {{Success: true, LatencyMs: 30}},
		},
		callIndex: map[int64]int{},
	}
	svc, repo := newAdaptiveMonitorTestService(t, &ChannelMonitorAccountProbeState{
		MonitorID: 11, Model: "gpt-test", AccountID: &accountID, AccountName: "line-1",
		FinalStatus: MonitorStatusOperational,
	}, executor)

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].ProbeMode != accountProbeModeSticky {
		detail := ""
		if len(results) == 1 && results[0] != nil {
			detail = results[0].Status + " " + results[0].Message
		}
		t.Fatalf("result = %#v (%s), want one sticky probe", results, detail)
	}
	if executor.count(1) != 1 || executor.count(2) != 0 {
		t.Fatalf("calls = line1:%d line2:%d, want 1/0", executor.count(1), executor.count(2))
	}
	if len(repo.rows) != 1 || repo.rows[0].AccountName != "line-1" {
		t.Fatalf("persisted history = %#v, want sticky account source", repo.rows)
	}
}

func TestAdaptiveAccountProbePassesPublicModelAndAttributesMappedCostModel(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {{Success: true, LatencyMs: 30, RequestAttempted: true}},
		},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	account := Account{
		ID:       1,
		Name:     "deepseek-line",
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-deepseek",
			"model_mapping": map[string]any{
				"public-model":   "upstream-model",
				"upstream-model": "incorrect-second-map",
			},
		},
	}

	result := svc.probeAdaptiveAccount(
		context.Background(),
		&ChannelMonitor{ID: 11, Provider: MonitorProviderDeepSeek, PrimaryModel: "public-model"},
		"public-model",
		account,
		accountProbeModeFull,
		ChannelMonitorAccountProbeSettings{DegradedThresholdMs: 6000},
	)

	if len(executor.calls) != 1 || executor.calls[0].model != "public-model" {
		t.Fatalf("executor calls = %#v, want one public-model probe", executor.calls)
	}
	if result.monitorCostModel != "upstream-model" {
		t.Fatalf("monitorCostModel = %q, want mapped upstream model", result.monitorCostModel)
	}
}

func TestAdaptiveAccountProbeUsesLowCostChatCompletionsForGroupedAccounts(t *testing.T) {
	accountID := int64(1)
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, repo := newAdaptiveMonitorTestService(t, &ChannelMonitorAccountProbeState{
		MonitorID: 11, Model: "gpt-test", AccountID: &accountID, AccountName: "line-1",
		FinalStatus: MonitorStatusOperational,
	}, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.accounts = []Account{
		openAIProbeAccount(1, "line-1", 1, map[string]any{"gpt-test": "upstream-gpt-test"}),
		openAIProbeAccount(2, "line-2", 2, nil),
	}
	upstream := &accountProbeHTTPStub{}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].ProbeMode != accountProbeModeSticky || results[0].Status != MonitorStatusOperational {
		t.Fatalf("results = %#v, want one sticky operational probe", results)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("legacy account-test executor should stay unused, calls=%#v", executor.calls)
	}
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	req := upstream.requests[1]
	if req.path != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", req.path)
	}
	if req.auth != "Bearer sk-line-1" {
		t.Fatalf("auth = %q, want grouped account API key", req.auth)
	}
	if req.body["model"] != "upstream-gpt-test" {
		t.Fatalf("model = %#v, want mapped upstream model", req.body["model"])
	}
	if req.body["max_tokens"] != float64(monitorLowCostMaxTokens) {
		t.Fatalf("max_tokens = %#v, want %d", req.body["max_tokens"], monitorLowCostMaxTokens)
	}
	if req.body["stream"] != false {
		t.Fatalf("stream = %#v, want false", req.body["stream"])
	}
	if _, ok := req.body["instructions"]; ok {
		t.Fatalf("low-cost probe must not send full instructions: %#v", req.body)
	}
	if len(repo.rows) != 1 || repo.rows[0].AccountName != "line-1" {
		t.Fatalf("persisted history = %#v, want sticky grouped account", repo.rows)
	}
}

func TestAdaptiveAccountProbeUsesLowCostAnthropicAPIKeyRequest(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, repo := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformAnthropic
	proxyID := int64(9)
	accountRepo.accounts = []Account{{
		ID:          101,
		Name:        "cc-line",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
		Priority:    1,
		ProxyID:     &proxyID,
		Proxy: &Proxy{
			ID:       proxyID,
			Protocol: "http",
			Host:     "proxy.example.com",
			Port:     3128,
			Status:   StatusActive,
		},
		Credentials: map[string]any{
			"api_key":  "sk-cc",
			"base_url": "https://cc.example.com",
			"model_mapping": map[string]any{
				"claude-sonnet-5": "P6.1-claude-sonnet-5",
			},
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"x-cc-route": "kiro",
			},
		},
	}}
	upstream := &anthropicAdaptiveHTTPStub{}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)

	monitor := repo.monitors[11]
	monitor.Provider = MonitorProviderAnthropic
	monitor.PrimaryModel = "claude-sonnet-5"

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusOperational {
		t.Fatalf("results = %#v, want one operational Anthropic result", results)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("legacy full account test must stay unused, calls=%#v", executor.calls)
	}
	if results[0].monitorCostModel != "P6.1-claude-sonnet-5" {
		t.Fatalf("monitor cost model = %q, want mapped model", results[0].monitorCostModel)
	}
	if !results[0].monitorUsage.Observed ||
		results[0].monitorUsage.Tokens.InputTokens != 3 ||
		results[0].monitorUsage.Tokens.OutputTokens != 1 {
		t.Fatalf("monitor usage = %#v, want observed 3/1 tokens", results[0].monitorUsage)
	}

	request := upstream.latestRequest(t)
	if request.url != "https://cc.example.com/v1/messages" {
		t.Fatalf("request URL = %q", request.url)
	}
	if request.headers.Get("x-api-key") != "sk-cc" {
		t.Fatalf("x-api-key = %q", request.headers.Get("x-api-key"))
	}
	if request.headers.Get("anthropic-version") != monitorAnthropicAPIVersion {
		t.Fatalf("anthropic-version = %q", request.headers.Get("anthropic-version"))
	}
	if adaptiveHeaderValue(request.headers, "x-cc-route") != "kiro" {
		t.Fatalf("header override = %q", adaptiveHeaderValue(request.headers, "x-cc-route"))
	}
	if request.headers.Get(usagesource.Header) != usagesource.ChannelMonitor ||
		request.headers.Get(usagesource.SignatureHeader) == "" {
		t.Fatalf("request is missing trusted channel monitor headers")
	}
	if request.proxyURL != "http://proxy.example.com:3128" || request.accountID != 101 || request.concurrency != 3 {
		t.Fatalf("request transport metadata = %#v", request)
	}
	if request.body["model"] != "P6.1-claude-sonnet-5" {
		t.Fatalf("body model = %#v", request.body["model"])
	}
	if request.body["max_tokens"] != float64(monitorLowCostMaxTokens) || request.body["stream"] != false {
		t.Fatalf("low-cost body = %#v", request.body)
	}
	if _, ok := request.body["system"]; ok {
		t.Fatalf("low-cost Anthropic probe must not include a full system prompt: %#v", request.body)
	}
	messages, ok := request.body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", request.body["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["content"] != monitorLowCostChallengePrompt {
		t.Fatalf("probe message = %#v", messages[0])
	}
}

func TestAdaptiveAccountProbeAnthropicAPIKeyReportsUpstreamFailure(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, repo := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformAnthropic
	accountRepo.accounts = []Account{{
		ID:          102,
		Name:        "cc-failed",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-cc-failed",
			"base_url": "https://cc.example.com",
		},
	}}
	upstream := &anthropicAdaptiveHTTPStub{
		status:       http.StatusBadGateway,
		responseBody: `{"error":{"message":"temporary upstream failure"}}`,
	}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)
	monitor := repo.monitors[11]
	monitor.Provider = MonitorProviderAnthropic
	monitor.PrimaryModel = "claude-sonnet-5"

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusError {
		t.Fatalf("results = %#v, want one Anthropic error", results)
	}
	if !strings.Contains(results[0].Message, "upstream HTTP 502") ||
		!strings.Contains(results[0].Message, "temporary upstream failure") {
		t.Fatalf("error message = %q", results[0].Message)
	}
}

func TestAdaptiveAccountProbeUsesLowCostGeminiAPIKeyRequest(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, repo := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformGemini
	proxyID := int64(12)
	accountRepo.accounts = []Account{{
		ID:          120,
		Name:        "gemini-line",
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 4,
		Priority:    1,
		ProxyID:     &proxyID,
		Proxy: &Proxy{
			ID:       proxyID,
			Protocol: "http",
			Host:     "proxy.example.com",
			Port:     3128,
			Status:   StatusActive,
		},
		Credentials: map[string]any{
			"api_key":  "AIza-gemini",
			"base_url": "https://gemini.example.com",
			"model_mapping": map[string]any{
				"gemini-3.7-flash": "upstream-model",
			},
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"x-gemini-route": "line-a",
			},
		},
	}}
	upstream := &geminiAdaptiveHTTPStub{}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)

	monitor := repo.monitors[11]
	monitor.Provider = MonitorProviderGemini
	monitor.PrimaryModel = "gemini-3.7-flash"

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusOperational {
		t.Fatalf("results = %#v, want one operational Gemini result", results)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("legacy account test must stay unused, calls=%#v", executor.calls)
	}
	if results[0].monitorCostModel != "upstream-model" {
		t.Fatalf("monitor cost model = %q, want mapped model", results[0].monitorCostModel)
	}
	if !results[0].monitorUsage.Observed ||
		results[0].monitorUsage.Tokens.InputTokens != 3 ||
		results[0].monitorUsage.Tokens.OutputTokens != 1 ||
		results[0].monitorUsage.Tokens.CacheReadTokens != 1 {
		t.Fatalf("monitor usage = %#v, want observed 3/1 with one cached token", results[0].monitorUsage)
	}

	request := upstream.latestRequest(t)
	if request.url != "https://gemini.example.com/v1beta/models/upstream-model:generateContent" {
		t.Fatalf("request URL = %q", request.url)
	}
	if request.headers.Get("x-goog-api-key") != "AIza-gemini" {
		t.Fatalf("x-goog-api-key = %q", request.headers.Get("x-goog-api-key"))
	}
	if adaptiveHeaderValue(request.headers, "x-gemini-route") != "line-a" {
		t.Fatalf("header override = %q", adaptiveHeaderValue(request.headers, "x-gemini-route"))
	}
	if request.headers.Get(usagesource.Header) != usagesource.ChannelMonitor ||
		request.headers.Get(usagesource.SignatureHeader) == "" {
		t.Fatalf("request is missing trusted channel monitor headers")
	}
	if request.proxyURL != "http://proxy.example.com:3128" || request.accountID != 120 || request.concurrency != 4 {
		t.Fatalf("request transport metadata = %#v", request)
	}
	if request.body["contents"] == nil {
		t.Fatalf("Gemini body is missing contents: %#v", request.body)
	}
	generationConfig, ok := request.body["generationConfig"].(map[string]any)
	if !ok || generationConfig["maxOutputTokens"] != float64(monitorLowCostMaxTokens) {
		t.Fatalf("low-cost generation config = %#v", request.body["generationConfig"])
	}
}

func TestAdaptiveAccountProbeGeminiAPIKeyReportsUpstreamFailure(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, repo := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformGemini
	accountRepo.accounts = []Account{{
		ID:          121,
		Name:        "gemini-failed",
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "AIza-failed",
			"base_url": "https://gemini.example.com",
		},
	}}
	upstream := &geminiAdaptiveHTTPStub{
		status:       http.StatusBadGateway,
		responseBody: `{"error":{"message":"temporary Gemini upstream failure"}}`,
	}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)
	monitor := repo.monitors[11]
	monitor.Provider = MonitorProviderGemini
	monitor.PrimaryModel = "gemini-3.7-flash"

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusError {
		t.Fatalf("results = %#v, want one Gemini error", results)
	}
	if !strings.Contains(results[0].Message, "upstream HTTP 502") ||
		!strings.Contains(results[0].Message, "temporary Gemini upstream failure") {
		t.Fatalf("error message = %q", results[0].Message)
	}
}

func TestAdaptiveAccountProbeKeepsUnsupportedGeminiAccountTypeError(t *testing.T) {
	svc, _ := newAdaptiveMonitorTestService(t, nil, &adaptiveProbeExecutorStub{
		results:   map[int64][]AccountMonitorProbeResult{},
		callIndex: map[int64]int{},
	})
	svc.SetAccountProbeDependencies(
		svc.accountProbeRepo,
		&geminiAdaptiveHTTPStub{},
		&config.Config{},
		nil,
	)
	account := &Account{
		ID:       122,
		Name:     "gemini-oauth",
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	result := svc.runLowCostAdaptiveAccountProbe(
		context.Background(),
		&ChannelMonitor{Provider: MonitorProviderGemini},
		"gemini-3.7-flash",
		account,
	)
	if result == nil || result.Status != MonitorStatusError || result.monitorRequestAttempted {
		t.Fatalf("result = %#v, want an unattempted unsupported-account error", result)
	}
	if !strings.Contains(result.Message, "does not support low-cost adaptive probing") {
		t.Fatalf("error message = %q", result.Message)
	}
}

func TestAdaptiveAccountProbeUsesLinkedGroupPlatformForCompatibleProvider(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {{Success: true, LatencyMs: 30}},
			2: {{Success: true, LatencyMs: 20}},
		},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformDeepSeek
	for i := range accountRepo.accounts {
		accountRepo.accounts[i].Platform = PlatformDeepSeek
	}

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusOperational {
		t.Fatalf("results = %#v, want one operational result", results)
	}
	if len(accountRepo.calls) != 1 || accountRepo.calls[0].platform != PlatformDeepSeek {
		t.Fatalf("account repo calls = %#v, want linked DeepSeek group platform", accountRepo.calls)
	}
}

func TestAdaptiveAccountProbeExplicitCompatibleProviderRequiresMatchingAccountPlatform(t *testing.T) {
	for _, provider := range []string{
		MonitorProviderDeepSeek,
		MonitorProviderKimi,
		MonitorProviderGLM,
	} {
		t.Run(provider, func(t *testing.T) {
			executor := &adaptiveProbeExecutorStub{
				results: map[int64][]AccountMonitorProbeResult{
					1: {{Success: true, LatencyMs: 30}},
					2: {{Success: true, LatencyMs: 20}},
				},
				callIndex: map[int64]int{},
			}
			svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
			accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
			accountRepo.groupPlatform = provider
			for i := range accountRepo.accounts {
				accountRepo.accounts[i].Platform = provider
			}

			monitor := svc.repo.(*adaptiveMonitorRepoStub).monitors[11]
			monitor.Provider = provider

			results, err := svc.RunCheck(context.Background(), 11)
			if err != nil {
				t.Fatalf("RunCheck: %v", err)
			}
			if len(results) != 1 || results[0].Status != MonitorStatusOperational {
				t.Fatalf("results = %#v, want one operational result", results)
			}
			if len(accountRepo.calls) != 1 || accountRepo.calls[0].platform != provider {
				t.Fatalf("account repo calls = %#v, want platform %q", accountRepo.calls, provider)
			}
		})
	}
}

func TestAdaptiveAccountProbeCompatibleProviderUsesShortStableOutputBudget(t *testing.T) {
	for _, provider := range []string{
		MonitorProviderDeepSeek,
		MonitorProviderKimi,
		MonitorProviderGLM,
	} {
		t.Run(provider, func(t *testing.T) {
			executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
			svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
			accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
			accountRepo.groupPlatform = provider
			accountRepo.accounts = []Account{openAIProbeAccount(1, "compatible-line", 1, nil)}
			accountRepo.accounts[0].Platform = provider
			upstream := &accountProbeHTTPStub{}
			svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)

			monitor := svc.repo.(*adaptiveMonitorRepoStub).monitors[11]
			monitor.Provider = provider

			results, err := svc.RunCheck(context.Background(), 11)
			if err != nil {
				t.Fatalf("RunCheck: %v", err)
			}
			if len(results) != 1 || results[0].Status != MonitorStatusOperational {
				t.Fatalf("results = %#v, want one operational result", results)
			}

			upstream.mu.Lock()
			defer upstream.mu.Unlock()
			req := upstream.requests[1]
			if req.body["max_tokens"] != float64(monitorCompatibleLowCostMaxTokens) {
				t.Fatalf("max_tokens = %#v, want %d", req.body["max_tokens"], monitorCompatibleLowCostMaxTokens)
			}
		})
	}
}

func TestAdaptiveAccountProbeOpenAIProviderUsesLinkedCompatibleAccountBudget(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{results: map[int64][]AccountMonitorProbeResult{}, callIndex: map[int64]int{}}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformDeepSeek
	accountRepo.accounts = []Account{openAIProbeAccount(1, "deepseek-line", 1, nil)}
	accountRepo.accounts[0].Platform = PlatformDeepSeek
	upstream := &accountProbeHTTPStub{}
	svc.SetAccountProbeDependencies(accountRepo, upstream, &config.Config{}, nil)

	monitor := svc.repo.(*adaptiveMonitorRepoStub).monitors[11]
	monitor.Provider = MonitorProviderOpenAI

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusOperational {
		t.Fatalf("results = %#v, want one operational result", results)
	}

	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	req := upstream.requests[1]
	if req.body["max_tokens"] != float64(monitorCompatibleLowCostMaxTokens) {
		t.Fatalf("max_tokens = %#v, want %d", req.body["max_tokens"], monitorCompatibleLowCostMaxTokens)
	}
}

func TestAdaptiveAccountProbeExplicitCompatibleProviderRejectsDifferentAccountPlatform(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{
		results:   map[int64][]AccountMonitorProbeResult{},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformKimi
	for i := range accountRepo.accounts {
		accountRepo.accounts[i].Platform = PlatformKimi
	}

	monitor := svc.repo.(*adaptiveMonitorRepoStub).monitors[11]
	monitor.Provider = MonitorProviderDeepSeek

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusError ||
		!strings.Contains(results[0].Message, "incompatible") {
		t.Fatalf("results = %#v, want incompatible-platform error", results)
	}
	if len(accountRepo.calls) != 0 {
		t.Fatalf("account lookup calls = %#v, want none after platform rejection", accountRepo.calls)
	}
}

func TestAdaptiveAccountProbeRejectsIncompatibleLinkedGroupPlatform(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{
		results:   map[int64][]AccountMonitorProbeResult{},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	accountRepo := svc.accountProbeRepo.(*accountProbeRepoStub)
	accountRepo.groupPlatform = PlatformAnthropic

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Status != MonitorStatusError ||
		!strings.Contains(results[0].Message, "incompatible") {
		t.Fatalf("results = %#v, want incompatible-platform error", results)
	}
	if len(accountRepo.calls) != 0 {
		t.Fatalf("account lookup calls = %#v, want none after platform rejection", accountRepo.calls)
	}
}

func TestAdaptiveAccountProbeConfirmsThenSweepsOnlyFailedModel(t *testing.T) {
	accountID := int64(1)
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {
				{Success: false, LatencyMs: 20, Message: "first failure"},
				{Success: false, LatencyMs: 21, Message: "confirmed failure"},
				{Success: false, LatencyMs: 22, Message: "full sweep failure"},
			},
			2: {{Success: true, LatencyMs: 10, Message: "healthy"}},
		},
		callIndex: map[int64]int{},
	}
	svc, repo := newAdaptiveMonitorTestService(t, &ChannelMonitorAccountProbeState{
		MonitorID: 11, Model: "gpt-test", AccountID: &accountID, AccountName: "line-1",
		FinalStatus: MonitorStatusOperational,
	}, executor)

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	result := results[0]
	if result.Status != MonitorStatusOperational || result.ProbeMode != accountProbeModeFull ||
		result.AccountID == nil || *result.AccountID != 2 {
		t.Fatalf("result = %#v, want full-sweep line-2 operational", result)
	}
	if result.CandidateCount != 2 || result.HealthyCount != 1 {
		t.Fatalf("result counts = %d/%d, want 1 healthy of 2", result.HealthyCount, result.CandidateCount)
	}
	if executor.count(1) != 3 || executor.count(2) != 1 {
		t.Fatalf("calls = line1:%d line2:%d, want 3/1", executor.count(1), executor.count(2))
	}
	state, err := repo.GetAccountProbeState(context.Background(), 11, "gpt-test")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state == nil || state.AccountID == nil || *state.AccountID != 2 ||
		state.FinalStatus != MonitorStatusOperational || state.LastFullSweepAt == nil {
		t.Fatalf("state = %#v, want operational line-2 full-sweep state", state)
	}
}

func TestAdaptiveAccountProbeKeepsAbnormalStateOnFullSweepAndRepeatsNextRun(t *testing.T) {
	accountID := int64(1)
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {
				{Success: false, LatencyMs: 10, Message: "down"},
				{Success: false, LatencyMs: 10, Message: "down"},
			},
			2: {
				{Success: false, LatencyMs: 11, Message: "down"},
				{Success: false, LatencyMs: 11, Message: "down"},
			},
		},
		callIndex: map[int64]int{},
	}
	svc, repo := newAdaptiveMonitorTestService(t, &ChannelMonitorAccountProbeState{
		MonitorID: 11, Model: "gpt-test", AccountID: &accountID, AccountName: "line-1",
		FinalStatus: MonitorStatusFailed,
	}, executor)

	for run := 0; run < 2; run++ {
		results, err := svc.RunCheck(context.Background(), 11)
		if err != nil {
			t.Fatalf("run %d: %v", run+1, err)
		}
		if len(results) != 1 || results[0].ProbeMode != accountProbeModeFull {
			t.Fatalf("run %d result = %#v, want full sweep", run+1, results)
		}
	}
	if executor.count(1) != 2 || executor.count(2) != 2 {
		t.Fatalf("calls = line1:%d line2:%d, want both accounts on both runs", executor.count(1), executor.count(2))
	}
	state, err := repo.GetAccountProbeState(context.Background(), 11, "gpt-test")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state == nil || state.FinalStatus != MonitorStatusFailed || state.LastFullSweepAt == nil {
		t.Fatalf("state = %#v, want retained abnormal full-sweep state", state)
	}
}

func TestAdaptiveImageProbeRotatesNextCycleWithoutFanout(t *testing.T) {
	accountID := int64(1)
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {{Success: false, LatencyMs: 30, Message: "image down"}},
			2: {{Success: true, LatencyMs: 31, Message: "image healthy"}},
		},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, &ChannelMonitorAccountProbeState{
		MonitorID: 11, Model: "gpt-test", AccountID: &accountID, AccountName: "line-1",
		FinalStatus: MonitorStatusOperational,
	}, executor)
	monitor, err := svc.repo.GetByID(context.Background(), 11)
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	monitor.APIMode = MonitorAPIModeImages
	baseRepo := svc.repo.(*adaptiveMonitorRepoStub)
	baseRepo.monitors[11] = monitor

	firstResults, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(firstResults) != 1 || firstResults[0].ProbeMode != accountProbeModeSticky ||
		firstResults[0].Status != MonitorStatusFailed {
		t.Fatalf("first result = %#v, want one failed sticky image probe", firstResults)
	}
	if executor.count(1) != 1 || executor.count(2) != 0 {
		t.Fatalf("first-cycle image calls = line1:%d line2:%d, want 1/0", executor.count(1), executor.count(2))
	}

	secondResults, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("second RunCheck: %v", err)
	}
	if len(secondResults) != 1 || secondResults[0].ProbeMode != accountProbeModeFull ||
		secondResults[0].Status != MonitorStatusOperational ||
		secondResults[0].AccountID == nil || *secondResults[0].AccountID != 2 {
		t.Fatalf("second result = %#v, want one operational rotated image probe", secondResults)
	}
	if executor.count(1) != 1 || executor.count(2) != 1 {
		t.Fatalf("two-cycle image calls = line1:%d line2:%d, want 1/1", executor.count(1), executor.count(2))
	}
}

func TestAdaptiveImageProbeWithoutFanoutOnlyChecksPrimaryModel(t *testing.T) {
	executor := &adaptiveProbeExecutorStub{
		results: map[int64][]AccountMonitorProbeResult{
			1: {{Success: true, LatencyMs: 30, Message: "image healthy"}},
		},
		callIndex: map[int64]int{},
	}
	svc, _ := newAdaptiveMonitorTestService(t, nil, executor)
	monitor, err := svc.repo.GetByID(context.Background(), 11)
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	monitor.APIMode = MonitorAPIModeImages
	monitor.ExtraModels = []string{"gpt-image-extra"}
	svc.repo.(*adaptiveMonitorRepoStub).monitors[11] = monitor

	results, err := svc.RunCheck(context.Background(), 11)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if len(results) != 1 || results[0].Model != monitor.PrimaryModel {
		t.Fatalf("results = %#v, want only primary image model", results)
	}
	if len(executor.calls) != 1 || executor.calls[0].model != monitor.PrimaryModel {
		t.Fatalf("image calls = %#v, want one primary-model request", executor.calls)
	}
}

func TestLimitAdaptiveProbeAccountsKeepsStickyAccountInsideCandidateCap(t *testing.T) {
	accountID := int64(3)
	accounts := []Account{{ID: 1}, {ID: 2}, {ID: 3}}
	limited := limitAdaptiveProbeAccounts(accounts, &ChannelMonitorAccountProbeState{AccountID: &accountID}, 2)
	if len(limited) != 2 || limited[0].ID != 1 || limited[1].ID != 3 {
		t.Fatalf("limited accounts = %#v, want highest priority + sticky account", limited)
	}
}

func TestAdaptiveStateRepositoryPreservesFullSweepTimestamp(t *testing.T) {
	now := time.Now().UTC()
	repo := &adaptiveMonitorRepoStub{
		groupProbeRepoStub: &groupProbeRepoStub{},
		states: map[string]*ChannelMonitorAccountProbeState{
			adaptiveStateKey(1, "gpt-test"): {
				MonitorID: 1, Model: "gpt-test", LastFullSweepAt: &now,
			},
		},
	}
	if err := repo.UpsertAccountProbeState(context.Background(), &ChannelMonitorAccountProbeState{
		MonitorID: 1, Model: "gpt-test", FinalStatus: MonitorStatusOperational,
	}); err != nil {
		t.Fatalf("UpsertAccountProbeState: %v", err)
	}
	state, err := repo.GetAccountProbeState(context.Background(), 1, "gpt-test")
	if err != nil {
		t.Fatalf("GetAccountProbeState: %v", err)
	}
	if state == nil || state.LastFullSweepAt == nil || !state.LastFullSweepAt.Equal(now) {
		t.Fatalf("state = %#v, want preserved full sweep timestamp", state)
	}
}
