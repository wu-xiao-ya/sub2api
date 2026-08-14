package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"golang.org/x/sync/errgroup"
)

type channelMonitorAccountProbeRepository interface {
	ListSchedulableByGroupNameAndPlatform(ctx context.Context, groupName, platform string) ([]Account, error)
	ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error)
	GetGroupPlatform(ctx context.Context, groupID int64) (string, error)
}

func (s *ChannelMonitorService) SetAccountProbeDependencies(
	accountRepo channelMonitorAccountProbeRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
) {
	s.accountProbeRepo = accountRepo
	s.httpUpstream = httpUpstream
	s.cfg = cfg
	s.tlsFPProfileService = tlsFPProfileService
}

func (s *ChannelMonitorService) runBusinessGroupProbeIfConfigured(
	ctx context.Context,
	m *ChannelMonitor,
) ([]*CheckResult, bool) {
	accounts, groupName, matched, err := s.resolveBusinessGroupProbeAccounts(ctx, m)
	if err != nil {
		result := newMonitorAccountProbeErrorResult(m, fmt.Sprintf("account group probe lookup failed: %v", err))
		return []*CheckResult{result}, true
	}
	if !matched {
		return nil, false
	}
	if len(accounts) == 0 {
		result := newMonitorAccountProbeErrorResult(m, fmt.Sprintf(
			"account group %q has no eligible schedulable OpenAI API key accounts", groupName,
		))
		return []*CheckResult{result}, true
	}

	probes := s.runBusinessGroupProbeChecks(ctx, m, accounts)
	best := selectBestAccountProbe(probes)
	if best == nil || best.result == nil {
		result := newMonitorAccountProbeErrorResult(m, fmt.Sprintf(
			"account group %q probe produced no result", groupName,
		))
		return []*CheckResult{result}, true
	}

	result := cloneCheckResult(best.result)
	accountID := best.account.ID
	result.AccountID = &accountID
	result.AccountName = strings.TrimSpace(best.account.Name)
	result.ProbeMode = accountProbeModeFull
	result.CandidateCount = len(probes)
	result.HealthyCount = countHealthyAccountProbes(probes)
	result.Model = m.PrimaryModel
	result.Message = decorateAccountProbeMessage(result.Message, accountProbeSummaryMessage(groupName, probes, best))
	slog.Info("channel_monitor: account group probe complete",
		"monitor_id", m.ID,
		"group_name", groupName,
		"candidates", len(probes),
		"successful", countHealthyAccountProbes(probes),
		"best_account_id", best.account.ID,
		"best_account_name", best.account.Name,
		"best_status", result.Status,
		"best_latency_ms", result.LatencyMs,
	)
	return []*CheckResult{result}, true
}

func (s *ChannelMonitorService) resolveBusinessGroupProbeAccounts(
	ctx context.Context,
	m *ChannelMonitor,
) ([]Account, string, bool, error) {
	if s.accountProbeRepo == nil || s.httpUpstream == nil || m == nil {
		return nil, "", false, nil
	}
	groupName := strings.TrimSpace(m.GroupName)
	if groupName == "" || strings.TrimSpace(m.Provider) != MonitorProviderOpenAI {
		return nil, groupName, false, nil
	}
	switch defaultAPIMode(m.APIMode) {
	case MonitorAPIModeChatCompletions, MonitorAPIModeResponses:
	default:
		return nil, groupName, false, nil
	}

	var (
		accounts []Account
		err      error
	)
	if m.AccountGroupID != nil && *m.AccountGroupID > 0 {
		platform, platformErr := s.accountProbeRepo.GetGroupPlatform(ctx, *m.AccountGroupID)
		if platformErr != nil {
			return nil, groupName, true, platformErr
		}
		accounts, err = s.accountProbeRepo.ListSchedulableByGroupIDAndPlatform(ctx, *m.AccountGroupID, platform)
	} else {
		accounts, err = s.accountProbeRepo.ListSchedulableByGroupNameAndPlatform(ctx, groupName, PlatformOpenAI)
	}
	if err != nil {
		return nil, groupName, true, err
	}
	eligible := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if !account.IsOpenAICompatible() || account.Type != AccountTypeAPIKey {
			continue
		}
		if strings.TrimSpace(account.GetOpenAIApiKey()) == "" {
			continue
		}
		eligible = append(eligible, account)
	}
	if len(eligible) > monitorGroupProbeMaxCandidates {
		eligible = eligible[:monitorGroupProbeMaxCandidates]
	}
	return eligible, groupName, true, nil
}

type accountProbeResult struct {
	account Account
	result  *CheckResult
}

func (s *ChannelMonitorService) runBusinessGroupProbeChecks(
	ctx context.Context,
	m *ChannelMonitor,
	accounts []Account,
) []accountProbeResult {
	results := make([]accountProbeResult, len(accounts))
	var eg errgroup.Group
	eg.SetLimit(monitorGroupProbeParallelism)

	for i := range accounts {
		i := i
		account := accounts[i]
		eg.Go(func() error {
			result := s.runOpenAIAPIKeyAccountProbe(ctx, m, &account)
			s.recordMonitorCost(ctx, m, result, &account)
			results[i] = accountProbeResult{
				account: account,
				result:  result,
			}
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

func (s *ChannelMonitorService) runOpenAIAPIKeyAccountProbe(
	ctx context.Context,
	m *ChannelMonitor,
	account *Account,
) *CheckResult {
	res := &CheckResult{
		Model:     m.PrimaryModel,
		Status:    MonitorStatusError,
		CheckedAt: time.Now(),
	}
	if account == nil {
		res.Message = "account group probe has no account"
		return res
	}
	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		res.Message = "account group probe skipped account with empty API key"
		return res
	}
	baseURL, err := s.validateAccountProbeBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf("invalid account base_url: %v", err)))
		return res
	}

	opts := accountProbeCheckOptions(m)
	challenge := generateChallengeForOptions(opts)
	req, apiMode, err := buildOpenAIAccountProbeRequest(ctx, baseURL, apiKey, m, account, challenge.Prompt, opts)
	if err != nil {
		res.Message = truncateMessage(sanitizeErrorMessage(err.Error()))
		return res
	}

	start := time.Now()
	res.monitorRequestAttempted = true
	resp, err := s.doAccountProbeRequest(req, account)
	latency := time.Since(start)
	latencyMs := int(latency / time.Millisecond)
	res.LatencyMs = &latencyMs
	res.PingLatencyMs = pingEndpointOrigin(ctx, baseURL)
	if err != nil {
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf("do request: %v", err)))
		return res
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, monitorResponseMaxBytes))
	if readErr != nil {
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf("read body: %v", readErr)))
		return res
	}
	res.monitorUsage = monitorUsageFromResponse(accountProbeProvider(m, account), apiMode, respBytes)
	res.monitorCostModel = strings.TrimSpace(account.GetMappedModel(m.PrimaryModel))
	if res.monitorCostModel == "" {
		res.monitorCostModel = m.PrimaryModel
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf(
			"upstream HTTP %d: %s", resp.StatusCode, truncateForErrorBody(string(respBytes)),
		)))
		return res
	}

	respText := extractAccountProbeText(apiMode, respBytes)
	if bodyOverrideMode(opts) == MonitorBodyOverrideModeReplace {
		if strings.TrimSpace(respText) == "" {
			res.Status = MonitorStatusFailed
			res.Message = truncateMessage("replace-mode: upstream returned 2xx with empty text")
			return res
		}
		return finalizeOperationalOrDegraded(res, latency, latencyMs)
	}
	if !validateMonitorChallengeResponse(accountProbeProvider(m, account), respText, respBytes, challenge.Expected, opts) {
		res.Status = MonitorStatusFailed
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf(
			"challenge mismatch (expected %s, got %q)", challenge.Expected, respText,
		)))
		return res
	}
	return finalizeOperationalOrDegraded(res, latency, latencyMs)
}

func (s *ChannelMonitorService) validateAccountProbeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "https://api.openai.com"
	}
	if s.cfg == nil {
		return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{})
	}
	if !s.cfg.Security.URLAllowlist.Enabled {
		return urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
	}
	return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
}

func accountProbeCheckOptions(m *ChannelMonitor) *CheckOptions {
	return &CheckOptions{
		APIMode:          m.APIMode,
		LowCost:          true,
		ExtraHeaders:     m.ExtraHeaders,
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
	}
}

func buildOpenAIAccountProbeRequest(
	ctx context.Context,
	baseURL, apiKey string,
	m *ChannelMonitor,
	account *Account,
	prompt string,
	opts *CheckOptions,
) (*http.Request, string, error) {
	requestedAPIMode := checkAPIMode(opts)
	switch requestedAPIMode {
	case MonitorAPIModeChatCompletions, MonitorAPIModeResponses:
	default:
		return nil, "", fmt.Errorf("account group probe does not support api_mode %q", opts.APIMode)
	}
	provider := accountProbeProvider(m, account)
	adapter, apiMode, ok := providerAdapterFor(provider, requestedAPIMode)
	if !ok {
		return nil, "", fmt.Errorf("account group probe does not support api_mode %q", opts.APIMode)
	}
	model := strings.TrimSpace(account.GetMappedModel(m.PrimaryModel))
	if model == "" {
		model = strings.TrimSpace(m.PrimaryModel)
	}
	body, err := buildRequestBody(adapter, provider, apiMode, model, prompt, opts)
	if err != nil {
		return nil, "", err
	}
	targetURL := buildOpenAIChatCompletionsURL(baseURL)
	if apiMode == MonitorAPIModeResponses {
		targetURL = buildOpenAIResponsesURL(baseURL)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		targetURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, value := range mergeHeaders(adapter.buildHeaders(apiKey), opts) {
		req.Header.Set(key, value)
	}
	account.ApplyHeaderOverrides(req.Header)
	usagesource.SetChannelMonitor(req.Header)
	return req, apiMode, nil
}

func accountProbeProvider(m *ChannelMonitor, account *Account) string {
	if m == nil {
		return MonitorProviderOpenAI
	}
	provider := strings.TrimSpace(m.Provider)
	switch provider {
	case MonitorProviderOpenAI,
		MonitorProviderGrok,
		MonitorProviderDeepSeek,
		MonitorProviderKimi,
		MonitorProviderGLM:
		if provider != MonitorProviderOpenAI || account == nil {
			return provider
		}
		switch account.Platform {
		case PlatformDeepSeek:
			return MonitorProviderDeepSeek
		case PlatformKimi:
			return MonitorProviderKimi
		case PlatformGLM:
			return MonitorProviderGLM
		default:
			return provider
		}
	default:
		return MonitorProviderOpenAI
	}
}

func (s *ChannelMonitorService) doAccountProbeRequest(req *http.Request, account *Account) (*http.Response, error) {
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if s.tlsFPProfileService != nil {
		return s.httpUpstream.DoWithTLS(
			req,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
}

func extractAccountProbeText(apiMode string, respBytes []byte) string {
	if apiMode == MonitorAPIModeResponses {
		return extractOpenAIResponsesText(respBytes)
	}
	return extractMonitorResponseText(providerOpenAIChatAdapter, respBytes)
}

func selectBestAccountProbe(probes []accountProbeResult) *accountProbeResult {
	bestIndex := -1
	for i := range probes {
		if probes[i].result == nil {
			continue
		}
		if bestIndex < 0 || isBetterAccountProbe(probes[i], probes[bestIndex]) {
			bestIndex = i
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return &probes[bestIndex]
}

func isBetterAccountProbe(candidate, incumbent accountProbeResult) bool {
	candidateRank := monitorStatusRank(candidate.result.Status)
	incumbentRank := monitorStatusRank(incumbent.result.Status)
	if candidateRank != incumbentRank {
		return candidateRank > incumbentRank
	}
	candidateLatency := monitorLatencySortValue(candidate.result.LatencyMs)
	incumbentLatency := monitorLatencySortValue(incumbent.result.LatencyMs)
	if candidateLatency != incumbentLatency {
		return candidateLatency < incumbentLatency
	}
	return candidate.account.ID < incumbent.account.ID
}

func countHealthyAccountProbes(probes []accountProbeResult) int {
	count := 0
	for _, probe := range probes {
		if probe.result != nil && isMonitorHealthyStatus(probe.result.Status) {
			count++
		}
	}
	return count
}

func accountProbeSummaryMessage(groupName string, probes []accountProbeResult, best *accountProbeResult) string {
	if best == nil {
		return fmt.Sprintf("account group %q probed %d accounts, no best account", groupName, len(probes))
	}
	return fmt.Sprintf(
		"account group %q probed %d accounts, %d healthy; best account #%d %s",
		groupName,
		len(probes),
		countHealthyAccountProbes(probes),
		best.account.ID,
		strings.TrimSpace(best.account.Name),
	)
}

func decorateAccountProbeMessage(message, summary string) string {
	message = strings.TrimSpace(message)
	summary = strings.TrimSpace(summary)
	if message == "" {
		return truncateMessage(summary)
	}
	return truncateMessage(message + "; " + summary)
}

func cloneCheckResult(in *CheckResult) *CheckResult {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func newMonitorAccountProbeErrorResult(m *ChannelMonitor, message string) *CheckResult {
	model := ""
	if m != nil {
		model = m.PrimaryModel
	}
	return &CheckResult{
		Model:     model,
		Status:    MonitorStatusError,
		Message:   truncateMessage(sanitizeErrorMessage(message)),
		CheckedAt: time.Now(),
	}
}
