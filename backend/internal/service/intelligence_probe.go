package service

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/google/uuid"
)

// The probe replays the well-known "pelican riding a bicycle" drawing test
// against configured groups so an operator can eyeball whether an upstream
// started serving a degraded model. Requests go through the internal gateway
// with a station monitoring key, i.e. exactly the path a real user request
// takes, including group scheduling and account selection.
const (
	IntelligenceProbeDefaultIntervalMinutes = 12
	IntelligenceProbeMinIntervalMinutes     = 1
	IntelligenceProbeMaxIntervalMinutes     = 24 * 60

	IntelligenceProbeDefaultRetentionDays = 1
	IntelligenceProbeMinRetentionDays     = 1
	IntelligenceProbeMaxRetentionDays     = 30

	intelligenceProbeCycleInterval = time.Minute
	// A pelican drawing through a real account routinely takes two minutes:
	// non-streaming generation plus the gateway's own upstream retry loop.
	intelligenceProbeRequestTimeout = 240 * time.Second
	// Streaming keeps headers flowing as soon as an upstream attempt succeeds,
	// but the gateway may spend a while in retries before that first byte, so
	// the header timeout stays generous and the context budget dominates.
	intelligenceProbeResponseHeaderTimeout = 230 * time.Second
	intelligenceProbeMaxBodyBytes          = 512 * 1024
	intelligenceProbeMaxTokens             = 8000
	intelligenceProbeExcerptBytes          = 500
	intelligenceProbeConcurrency           = 2
	intelligenceProbeKeepPerTarget         = 500

	intelligenceProbeLeaderLockKey  = "intelligence:probe:leader"
	intelligenceProbeLeaderLockTTL  = 2 * time.Minute
	intelligenceProbeOpenAIChatPath = "/v1/chat/completions"

	intelligenceProbeStatusSuccess = "success"
	intelligenceProbeStatusNoSVG   = "no_svg"
	intelligenceProbeStatusFailed  = "failed"

	intelligenceProbeMaxTargets = 20

	intelligenceProbeDefaultPrompt = "Draw a pelican riding a bicycle. " +
		"Respond with one complete <svg>...</svg> document and nothing else. " +
		"Make it detailed."
)

// intelligenceProbeSVGRegex extracts the first complete SVG document from a
// model reply. Models wrap the SVG in prose or code fences, so the match must
// not assume line boundaries.
var intelligenceProbeSVGRegex = regexp.MustCompile(`(?is)<svg[\s>].*</svg>`)

// intelligenceProbeHTTPClient dials the loopback gateway only, like the
// monitor client, but tolerates long non-streaming generations.
var intelligenceProbeHTTPClient = newLoopbackOnlyHTTPClient(
	intelligenceProbeRequestTimeout,
	intelligenceProbeResponseHeaderTimeout,
)

// IntelligenceProbeSettings controls the periodic degradation probe runner.
type IntelligenceProbeSettings struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes"`
	RetentionDays   int    `json:"retention_days"`
	PromptOverride  string `json:"prompt_override"`
}

// IntelligenceProbeTarget is one configured group+model pair.
type IntelligenceProbeTarget struct {
	ID            int64      `json:"id"`
	GroupID       int64      `json:"group_id"`
	GroupName     string     `json:"group_name"`
	GroupPlatform string     `json:"group_platform"`
	Model         string     `json:"model"`
	Enabled       bool       `json:"enabled"`
	LastRunAt     *time.Time `json:"last_run_at"`
	NextRunAt     *time.Time `json:"next_run_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// IntelligenceProbeResult is one stored probe outcome, including the SVG.
type IntelligenceProbeResult struct {
	ID              int64     `json:"id"`
	TargetID        int64     `json:"target_id"`
	Status          string    `json:"status"`
	ResponseSVG     string    `json:"response_svg"`
	ResponseExcerpt string    `json:"response_excerpt"`
	ErrorMessage    string    `json:"error_message"`
	LatencyMs       *int      `json:"latency_ms"`
	CreatedAt       time.Time `json:"created_at"`
}

// IntelligenceProbeUserResult joins a result with its target for the
// user-facing gallery (group name and model come from the target).
type IntelligenceProbeUserResult struct {
	ID              int64     `json:"id"`
	TargetID        int64     `json:"target_id"`
	GroupID         int64     `json:"group_id"`
	GroupName       string    `json:"group_name"`
	Model           string    `json:"model"`
	Status          string    `json:"status"`
	ResponseSVG     string    `json:"response_svg"`
	ResponseExcerpt string    `json:"response_excerpt"`
	ErrorMessage    string    `json:"error_message"`
	LatencyMs       *int      `json:"latency_ms"`
	CreatedAt       time.Time `json:"created_at"`
}

// IntelligenceProbeConfig is the admin-facing aggregate: runner settings plus
// the configured target list.
type IntelligenceProbeConfig struct {
	Settings IntelligenceProbeSettings  `json:"settings"`
	Targets  []*IntelligenceProbeTarget `json:"targets"`
}

// IntelligenceProbeRepository persists targets and probe results.
type IntelligenceProbeRepository interface {
	ListTargets(ctx context.Context, enabledOnly bool) ([]*IntelligenceProbeTarget, error)
	GetTarget(ctx context.Context, id int64) (*IntelligenceProbeTarget, error)
	TargetGroupPlatform(ctx context.Context, groupID int64) (string, error)
	SyncTargets(ctx context.Context, targets []*IntelligenceProbeTarget) ([]*IntelligenceProbeTarget, error)
	UpdateTargetAfterRun(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error
	InsertResult(ctx context.Context, result *IntelligenceProbeResult) error
	ListResultsByTarget(ctx context.Context, targetID int64, limit int) ([]*IntelligenceProbeResult, error)
	ListRecentResultsForUser(ctx context.Context, perTargetLimit int) ([]*IntelligenceProbeUserResult, error)
	DeleteResultsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	PruneResultsPerTarget(ctx context.Context, keep int) error
}

var (
	ErrIntelligenceProbeUnavailable   = errors.New("intelligence probe is unavailable")
	ErrIntelligenceProbeTargetInvalid = errors.New("intelligence probe target is invalid")
)

// GetIntelligenceProbeSettings returns defaults when the setting is absent.
func (s *SettingService) GetIntelligenceProbeSettings(ctx context.Context) (*IntelligenceProbeSettings, error) {
	defaults := defaultIntelligenceProbeSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyIntelligenceProbeSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get intelligence probe settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaults, nil
	}
	settings := *defaults
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return nil, fmt.Errorf("parse intelligence probe settings: %w", err)
	}
	normalizeIntelligenceProbeSettings(&settings)
	return &settings, nil
}

// SetIntelligenceProbeSettings validates and persists the runner settings.
func (s *SettingService) SetIntelligenceProbeSettings(ctx context.Context, settings *IntelligenceProbeSettings) error {
	if s == nil || s.settingRepo == nil {
		return ErrIntelligenceProbeUnavailable
	}
	if settings == nil {
		return fmt.Errorf("intelligence probe settings cannot be nil")
	}
	normalizeIntelligenceProbeSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal intelligence probe settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyIntelligenceProbeSettings, string(data))
}

func defaultIntelligenceProbeSettings() *IntelligenceProbeSettings {
	return &IntelligenceProbeSettings{
		Enabled:         false,
		IntervalMinutes: IntelligenceProbeDefaultIntervalMinutes,
		RetentionDays:   IntelligenceProbeDefaultRetentionDays,
	}
}

func normalizeIntelligenceProbeSettings(settings *IntelligenceProbeSettings) {
	if settings.IntervalMinutes < IntelligenceProbeMinIntervalMinutes {
		settings.IntervalMinutes = IntelligenceProbeMinIntervalMinutes
	}
	if settings.IntervalMinutes > IntelligenceProbeMaxIntervalMinutes {
		settings.IntervalMinutes = IntelligenceProbeMaxIntervalMinutes
	}
	if settings.RetentionDays < IntelligenceProbeMinRetentionDays {
		settings.RetentionDays = IntelligenceProbeMinRetentionDays
	}
	if settings.RetentionDays > IntelligenceProbeMaxRetentionDays {
		settings.RetentionDays = IntelligenceProbeMaxRetentionDays
	}
	settings.PromptOverride = strings.TrimSpace(settings.PromptOverride)
}

func (s *IntelligenceProbeSettings) prompt() string {
	if strings.TrimSpace(s.PromptOverride) != "" {
		return strings.TrimSpace(s.PromptOverride)
	}
	return intelligenceProbeDefaultPrompt
}

// IntelligenceProbeService periodically replays the pelican SVG test through
// the internal gateway for each enabled target.
type IntelligenceProbeService struct {
	repo           IntelligenceProbeRepository
	settingService *SettingService
	apiKeyService  *APIKeyService
	gatewayURL     string

	parentCtx    context.Context
	parentCancel context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	started      bool
	stopped      bool
	cycleMu      sync.Mutex
	runSlots     chan struct{}
	now          func() time.Time
	lockCache    LeaderLockCache
	db           *sql.DB
	instanceID   string
}

func NewIntelligenceProbeService(
	repo IntelligenceProbeRepository,
	settingService *SettingService,
	apiKeyService *APIKeyService,
) *IntelligenceProbeService {
	ctx, cancel := context.WithCancel(context.Background())
	return &IntelligenceProbeService{
		repo:           repo,
		settingService: settingService,
		apiKeyService:  apiKeyService,
		parentCtx:      ctx,
		parentCancel:   cancel,
		runSlots:       make(chan struct{}, intelligenceProbeConcurrency),
		now:            time.Now,
		instanceID:     uuid.NewString(),
	}
}

// SetRuntimeDependencies injects the loopback gateway URL and the leader-lock
// backends. Without a gateway URL scheduled probes stay idle but the config
// endpoints keep working.
func (s *IntelligenceProbeService) SetRuntimeDependencies(gatewayURL string, lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.gatewayURL = gatewayURL
	s.lockCache = lockCache
	s.db = db
}

func (s *IntelligenceProbeService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runLoop()
}

func (s *IntelligenceProbeService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.parentCancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *IntelligenceProbeService) runLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(intelligenceProbeCycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.parentCtx.Done():
			return
		case <-ticker.C:
			if err := s.RunDue(s.parentCtx); err != nil {
				logger.LegacyPrintf("service.intelligence_probe", "run_due_failed: err=%v", err)
			}
		}
	}
}

// RunDue probes every enabled target whose next_run_at has passed and then
// prunes results beyond the retention window.
func (s *IntelligenceProbeService) RunDue(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	settings, err := s.getSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	release, acquired, lockErr := s.tryAcquireLeaderLock(ctx, intelligenceProbeLeaderLockKey)
	if lockErr != nil {
		return fmt.Errorf("acquire intelligence probe leader lock: %w", lockErr)
	}
	if !acquired {
		return nil
	}
	defer release()

	targets, err := s.repo.ListTargets(ctx, true)
	if err != nil {
		return fmt.Errorf("list intelligence probe targets: %w", err)
	}
	now := s.currentTime()
	for _, target := range targets {
		if target.NextRunAt != nil && now.Before(*target.NextRunAt) {
			continue
		}
		select {
		case s.runSlots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		go func(target *IntelligenceProbeTarget) {
			defer func() { <-s.runSlots }()
			if _, err := s.probeTarget(ctx, target, settings); err != nil {
				logger.LegacyPrintf("service.intelligence_probe", "probe_failed: target_id=%d err=%v", target.ID, err)
			}
		}(target)
	}
	if err := s.cleanup(ctx, settings); err != nil {
		logger.LegacyPrintf("service.intelligence_probe", "cleanup_failed: err=%v", err)
	}
	return nil
}

// GetConfig returns the runner settings plus the configured targets.
func (s *IntelligenceProbeService) GetConfig(ctx context.Context) (*IntelligenceProbeConfig, error) {
	if s == nil || s.repo == nil {
		return nil, ErrIntelligenceProbeUnavailable
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := s.repo.ListTargets(ctx, false)
	if err != nil {
		return nil, err
	}
	return &IntelligenceProbeConfig{Settings: *settings, Targets: targets}, nil
}

// UpdateConfig validates and persists settings and replaces the target list.
func (s *IntelligenceProbeService) UpdateConfig(ctx context.Context, config *IntelligenceProbeConfig) (*IntelligenceProbeConfig, error) {
	if s == nil || s.repo == nil {
		return nil, ErrIntelligenceProbeUnavailable
	}
	if config == nil {
		return nil, fmt.Errorf("intelligence probe config cannot be nil")
	}
	if len(config.Targets) > intelligenceProbeMaxTargets {
		return nil, fmt.Errorf("%w: at most %d targets are supported, got %d",
			ErrIntelligenceProbeTargetInvalid, intelligenceProbeMaxTargets, len(config.Targets))
	}
	seen := make(map[string]struct{}, len(config.Targets))
	for _, target := range config.Targets {
		if target == nil || target.GroupID <= 0 {
			return nil, ErrIntelligenceProbeTargetInvalid
		}
		if strings.TrimSpace(target.Model) == "" {
			return nil, ErrIntelligenceProbeTargetInvalid
		}
		platform, err := s.repo.TargetGroupPlatform(ctx, target.GroupID)
		if err != nil {
			return nil, err
		}
		if platform != PlatformOpenAI {
			return nil, fmt.Errorf("%w: group %d uses platform %q, only %q groups are supported",
				ErrIntelligenceProbeTargetInvalid, target.GroupID, platform, PlatformOpenAI)
		}
		key := fmt.Sprintf("%d:%s", target.GroupID, strings.ToLower(strings.TrimSpace(target.Model)))
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: duplicate group/model %s", ErrIntelligenceProbeTargetInvalid, key)
		}
		seen[key] = struct{}{}
	}
	if err := s.settingService.SetIntelligenceProbeSettings(ctx, &config.Settings); err != nil {
		return nil, err
	}
	if _, err := s.repo.SyncTargets(ctx, config.Targets); err != nil {
		return nil, err
	}
	return s.GetConfig(ctx)
}

// RunTargetBackground performs one probe detached from the caller's context:
// a drawing takes minutes, so browser or proxy disconnects must not kill it
// mid-flight. The result lands in the gallery when it finishes.
func (s *IntelligenceProbeService) RunTargetBackground(targetID int64) (bool, error) {
	if s == nil || s.repo == nil {
		return false, ErrIntelligenceProbeUnavailable
	}
	target, err := s.repo.GetTarget(context.Background(), targetID)
	if err != nil {
		return false, err
	}
	settings, err := s.getSettings(context.Background())
	if err != nil {
		return false, err
	}
	go s.runDetachedProbe(target, settings)
	return true, nil
}

// RunEnabledBackground probes every enabled target in the background.
func (s *IntelligenceProbeService) RunEnabledBackground() (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrIntelligenceProbeUnavailable
	}
	targets, err := s.repo.ListTargets(context.Background(), true)
	if err != nil {
		return 0, err
	}
	settings, err := s.getSettings(context.Background())
	if err != nil {
		return 0, err
	}
	count := 0
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		count++
		go s.runDetachedProbe(target, settings)
	}
	return count, nil
}

func (s *IntelligenceProbeService) runDetachedProbe(target *IntelligenceProbeTarget, settings *IntelligenceProbeSettings) {
	ctx, cancel := context.WithTimeout(context.Background(), intelligenceProbeRequestTimeout+30*time.Second)
	defer cancel()
	if _, err := s.probeTarget(ctx, target, settings); err != nil {
		logger.LegacyPrintf("service.intelligence_probe", "manual_probe_failed: target_id=%d err=%v", target.ID, err)
	}
}

// ListResults returns stored results for one target, newest first.
func (s *IntelligenceProbeService) ListResults(ctx context.Context, targetID int64, limit int) ([]*IntelligenceProbeResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrIntelligenceProbeUnavailable
	}
	if limit <= 0 || limit > intelligenceProbeKeepPerTarget {
		limit = intelligenceProbeKeepPerTarget
	}
	return s.repo.ListResultsByTarget(ctx, targetID, limit)
}

// ListUserResults returns recent results for every enabled target for the
// user-facing gallery.
func (s *IntelligenceProbeService) ListUserResults(ctx context.Context, perTargetLimit int) ([]*IntelligenceProbeUserResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrIntelligenceProbeUnavailable
	}
	if perTargetLimit <= 0 || perTargetLimit > intelligenceProbeKeepPerTarget {
		perTargetLimit = intelligenceProbeKeepPerTarget
	}
	return s.repo.ListRecentResultsForUser(ctx, perTargetLimit)
}

func (s *IntelligenceProbeService) getSettings(ctx context.Context) (*IntelligenceProbeSettings, error) {
	if s.settingService == nil {
		return defaultIntelligenceProbeSettings(), nil
	}
	return s.settingService.GetIntelligenceProbeSettings(ctx)
}

func (s *IntelligenceProbeService) cleanup(ctx context.Context, settings *IntelligenceProbeSettings) error {
	cutoff := s.currentTime().AddDate(0, 0, -settings.RetentionDays)
	if _, err := s.repo.DeleteResultsOlderThan(ctx, cutoff); err != nil {
		return err
	}
	return s.repo.PruneResultsPerTarget(ctx, intelligenceProbeKeepPerTarget)
}

func (s *IntelligenceProbeService) probeTarget(
	ctx context.Context,
	target *IntelligenceProbeTarget,
	settings *IntelligenceProbeSettings,
) (*IntelligenceProbeResult, error) {
	now := s.currentTime()
	result := s.executeProbe(ctx, target, settings)
	if err := s.repo.InsertResult(ctx, result); err != nil {
		return nil, fmt.Errorf("insert intelligence probe result: %w", err)
	}
	nextRun := s.currentTime().Add(time.Duration(settings.IntervalMinutes) * time.Minute)
	if err := s.repo.UpdateTargetAfterRun(ctx, target.ID, now, nextRun); err != nil {
		return nil, fmt.Errorf("update intelligence probe target schedule: %w", err)
	}
	return result, nil
}

func (s *IntelligenceProbeService) executeProbe(
	ctx context.Context,
	target *IntelligenceProbeTarget,
	settings *IntelligenceProbeSettings,
) *IntelligenceProbeResult {
	result := &IntelligenceProbeResult{TargetID: target.ID}
	plainKey, err := s.monitoringKeyForGroup(ctx, target.GroupID)
	if err != nil {
		result.Status = intelligenceProbeStatusFailed
		result.ErrorMessage = truncateProbeMessage(err.Error())
		return result
	}
	text, latencyMs, httpStatus, err := s.postChatCompletion(ctx, plainKey, target.Model, settings.prompt())
	result.LatencyMs = latencyMs
	svg := intelligenceProbeSVGRegex.FindString(text)
	if svg != "" {
		// A complete SVG in the accumulated stream is a good drawing even if
		// the read was cut short by the timeout.
		result.Status = intelligenceProbeStatusSuccess
		result.ResponseSVG = svg
		return result
	}
	if err != nil {
		result.Status = intelligenceProbeStatusFailed
		if httpStatus > 0 {
			result.ErrorMessage = truncateProbeMessage(fmt.Sprintf("HTTP %d: %s", httpStatus, err.Error()))
		} else {
			result.ErrorMessage = truncateProbeMessage(err.Error())
		}
		return result
	}
	result.Status = intelligenceProbeStatusNoSVG
	result.ResponseExcerpt = truncateProbeMessage(strings.TrimSpace(text))
	return result
}

// monitoringKeyForGroup ensures a station monitoring key exists for the group
// and returns its plaintext credential.
func (s *IntelligenceProbeService) monitoringKeyForGroup(ctx context.Context, groupID int64) (string, error) {
	if s.apiKeyService == nil {
		return "", fmt.Errorf("api key service is unavailable")
	}
	ensured, err := s.apiKeyService.EnsureMonitoringMonitorKeys(ctx, []int64{groupID})
	if err != nil {
		return "", fmt.Errorf("ensure monitoring key: %w", err)
	}
	for _, item := range ensured.Items {
		if item.GroupID == groupID && item.PlainKey != "" {
			return item.PlainKey, nil
		}
	}
	keys, err := s.apiKeyService.ListMonitoringMonitorKeys(ctx)
	if err != nil {
		return "", fmt.Errorf("list monitoring keys: %w", err)
	}
	var keyID int64
	for _, key := range keys {
		if key.GroupID == groupID && key.Status == StatusAPIKeyActive {
			keyID = key.ID
			break
		}
	}
	if keyID <= 0 {
		return "", fmt.Errorf("no active monitoring key for group %d", groupID)
	}
	key, err := s.apiKeyService.GetByID(ctx, keyID)
	if err != nil {
		return "", fmt.Errorf("load monitoring key %d: %w", keyID, err)
	}
	if key == nil || strings.TrimSpace(key.Key) == "" {
		return "", fmt.Errorf("monitoring key %d has no credential", keyID)
	}
	return key.Key, nil
}

func (s *IntelligenceProbeService) postChatCompletion(
	ctx context.Context,
	apiKey, model, prompt string,
) (text string, latencyMs *int, httpStatus int, err error) {
	started := time.Now()
	defer func() {
		measured := int(time.Since(started).Milliseconds())
		latencyMs = &measured
	}()
	if s.gatewayURL == "" {
		return "", nil, 0, fmt.Errorf("internal gateway URL is not configured")
	}
	endpoint, err := internalMonitorGatewayURL(s.gatewayURL)
	if err != nil {
		return "", nil, 0, err
	}
	// Streaming on purpose: the drawing takes minutes, and non-streaming
	// requests only send headers once the whole body exists, which turns any
	// header-timeout into a hard cliff. With SSE the bytes flow as they are
	// generated and the context budget below is the only real limit.
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens": intelligenceProbeMaxTokens,
		"stream":     true,
	})
	if err != nil {
		return "", nil, 0, fmt.Errorf("marshal probe body: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, intelligenceProbeRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost,
		strings.TrimRight(endpoint, "/")+intelligenceProbeOpenAIChatPath, bytes.NewReader(body))
	if err != nil {
		return "", nil, 0, fmt.Errorf("build probe request: %w", err)
	}
	// Mark the request as synthetic channel-monitor traffic so the V2 real
	// traffic observation does not count the drawing test as user load.
	headers := usagesource.MarkChannelMonitor(map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Authorization": "Bearer " + apiKey,
	})
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := intelligenceProbeHTTPClient.Do(req)
	if err != nil {
		// Transport failure before any response: nothing to salvage.
		return "", nil, 0, fmt.Errorf("probe request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, intelligenceProbeMaxBodyBytes))
		return "", nil, resp.StatusCode, fmt.Errorf("upstream error: %s", truncateProbeMessage(string(respBytes)))
	}
	text, readErr := accumulateStreamingText(resp.Body)
	// Callers salvage partial text: a complete SVG in what already arrived is
	// a perfectly good drawing even if the budget ran out mid-generation.
	return text, nil, resp.StatusCode, readErr
}

// accumulateStreamingText reads an OpenAI-style SSE stream and concatenates
// choices[0].delta.content until [DONE], EOF, or a read error (a timeout
// surfaces as a non-nil error alongside whatever already arrived).
func accumulateStreamingText(body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), intelligenceProbeMaxBodyBytes)
	var parts []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		if payload == "" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				parts = append(parts, choice.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return strings.Join(parts, ""), err
	}
	return strings.Join(parts, ""), nil
}

func (s *IntelligenceProbeService) tryAcquireLeaderLock(ctx context.Context, key string) (func(), bool, error) {
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.lockCache != nil {
		acquired, err := s.lockCache.TryAcquireLeaderLock(lockCtx, key, s.instanceID, intelligenceProbeLeaderLockTTL)
		if err != nil {
			return nil, false, err
		}
		if !acquired {
			return nil, false, nil
		}
		return func() {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer releaseCancel()
			_ = s.lockCache.ReleaseLeaderLock(releaseCtx, key, s.instanceID)
		}, true, nil
	}
	if s.db != nil {
		return tryAcquireDBAdvisoryLockWithError(lockCtx, s.db, hashAdvisoryLockID(key))
	}
	return func() {}, true, nil
}

func (s *IntelligenceProbeService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func truncateProbeMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= intelligenceProbeExcerptBytes {
		return message
	}
	return message[:intelligenceProbeExcerptBytes]
}
