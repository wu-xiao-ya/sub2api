package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/errgroup"
)

// ChannelMonitorRepository 渠道监控数据访问接口。
// 入参/返回的指针类型均使用 service 包的 ChannelMonitor 模型，
// repository 实现负责与 ent 模型互转，并保持 api_key_encrypted 字段为密文。
type ChannelMonitorRepository interface {
	// CRUD
	Create(ctx context.Context, m *ChannelMonitor) error
	GetByID(ctx context.Context, id int64) (*ChannelMonitor, error)
	Update(ctx context.Context, m *ChannelMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error)
	FindByDuplicateOperationID(ctx context.Context, operationID string) (*ChannelMonitor, error)

	// 调度器辅助
	ListEnabled(ctx context.Context) ([]*ChannelMonitor, error)
	MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error
	InsertHistoryBatch(ctx context.Context, rows []*ChannelMonitorHistoryRow) error
	DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error)

	// 历史记录
	ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error)

	// 用户视图聚合
	ListLatestPerModel(ctx context.Context, monitorID int64) ([]*ChannelMonitorLatest, error)
	ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*ChannelMonitorAvailability, error)

	// 批量聚合（admin/user list 用，避免 N+1）
	ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*ChannelMonitorLatest, error)
	ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*ChannelMonitorAvailability, error)
	// ListRecentHistoryForMonitors 批量取多个 monitor 各自主模型（primaryModels[monitorID]）最近 perMonitorLimit 条历史。
	// 返回的 entry 已按 checked_at DESC 排序（最新在前），不含 message 字段。
	ListRecentHistoryForMonitors(ctx context.Context, ids []int64, primaryModels map[int64]string, perMonitorLimit int) (map[int64][]*ChannelMonitorHistoryEntry, error)

	// ---------- 聚合维护（OpsCleanupService 调用） ----------

	// UpsertDailyRollupsFor 把 targetDate 当天的明细按 (monitor_id, model, bucket_date)
	// 聚合到 channel_monitor_daily_rollups。targetDate 会被截断到日期；
	// 用 ON CONFLICT DO UPDATE 实现幂等回填，返回 upsert 影响的行数。
	UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error)
	// DeleteRollupsBefore 软删 bucket_date < beforeDate 的聚合行，返回删除行数。
	DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error)
	// LoadAggregationWatermark 读 watermark（id=1）。
	// 返回 nil 表示从未聚合过；watermark 表本身预期已存在单行（migration 110 写入）。
	LoadAggregationWatermark(ctx context.Context) (*time.Time, error)
	// UpdateAggregationWatermark 写 watermark（UPSERT 到 id=1）。
	UpdateAggregationWatermark(ctx context.Context, date time.Time) error
}

// ChannelMonitorService 渠道监控管理服务。
type ChannelMonitorService struct {
	repo                         ChannelMonitorRepository
	encryptor                    SecretEncryptor
	accountProbeRepo             channelMonitorAccountProbeRepository
	accountProbeExecutor         channelMonitorAccountProbeExecutor
	accountProbeSettingsProvider channelMonitorAccountProbeSettingsProvider
	trafficUsageRepo             channelMonitorTrafficUsageRepository
	trafficSettingsProvider      channelMonitorTrafficSettingsProvider
	httpUpstream                 HTTPUpstream
	cfg                          *config.Config
	tlsFPProfileService          *TLSFingerprintProfileService
	settingService               *SettingService
	channelService               *ChannelService
	billingService               *BillingService
	trafficObservationMu         sync.Mutex
	trafficObservationWrites     map[string]time.Time
	// scheduler 由 wire 通过 SetScheduler 注入；CRUD 后调用对应钩子即时同步任务。
	// 测试或未注入场景下保持 nil，所有钩子调用变为 no-op。
	scheduler MonitorScheduler
}

const maxChannelMonitorNameRunes = 100

// ChannelMonitorDuplicateOperationIDMetadataKey is stored in the existing
// extra_headers JSON column to avoid a schema migration. The colon makes it an
// invalid HTTP header name, and repository adapters remove it before exposing
// ExtraHeaders to the service layer.
const ChannelMonitorDuplicateOperationIDMetadataKey = "sub2api:duplicate_operation_id"

// NewChannelMonitorService 创建渠道监控服务实例。
func NewChannelMonitorService(repo ChannelMonitorRepository, encryptor SecretEncryptor) *ChannelMonitorService {
	return &ChannelMonitorService{
		repo:                     repo,
		encryptor:                encryptor,
		trafficObservationWrites: make(map[string]time.Time),
	}
}

// ---------- CRUD ----------

// List 列表查询（支持 provider/enabled/search 过滤 + 分页）。
// 返回的 ChannelMonitor.APIKey 已解密为明文，handler 层负责脱敏。
func (s *ChannelMonitorService) List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 20
	}
	items, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list channel monitors: %w", err)
	}
	for _, it := range items {
		s.decryptInPlace(it)
	}
	return items, total, nil
}

// Get 查询单个监控（解密 API Key）。
func (s *ChannelMonitorService) Get(ctx context.Context, id int64) (*ChannelMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.decryptInPlace(m)
	return m, nil
}

// Create 创建监控（内部加密 api_key）。
func (s *ChannelMonitorService) Create(ctx context.Context, p ChannelMonitorCreateParams) (*ChannelMonitor, error) {
	if err := validateCreateParams(p); err != nil {
		return nil, err
	}
	if err := validateBodyModeForProtocol(p.Provider, p.APIMode, p.BodyOverrideMode, p.BodyOverride); err != nil {
		return nil, err
	}
	if err := validateExtraHeaders(p.ExtraHeaders); err != nil {
		return nil, err
	}
	encrypted, err := s.encryptor.Encrypt(p.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}
	m := &ChannelMonitor{
		Name:                  strings.TrimSpace(p.Name),
		Provider:              p.Provider,
		APIMode:               defaultAPIMode(p.APIMode),
		Endpoint:              normalizeEndpoint(p.Endpoint),
		APIKey:                encrypted, // 注意：传入 repository 时该字段为密文
		PrimaryModel:          normalizeMonitorPrimaryModel(p.Provider, p.PrimaryModel),
		ExtraModels:           normalizeModels(p.ExtraModels),
		GroupName:             strings.TrimSpace(p.GroupName),
		AccountGroupID:        cloneInt64Pointer(p.AccountGroupID),
		Enabled:               p.Enabled,
		IntervalSeconds:       p.IntervalSeconds,
		JitterSeconds:         p.JitterSeconds,
		RequestTimeoutSeconds: defaultRequestTimeoutSeconds(p.APIMode, p.RequestTimeoutSeconds),
		CreatedBy:             p.CreatedBy,
		TemplateID:            p.TemplateID,
		ExtraHeaders:          emptyHeadersIfNil(p.ExtraHeaders),
		BodyOverrideMode:      defaultBodyMode(p.BodyOverrideMode),
		BodyOverride:          p.BodyOverride,
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create channel monitor: %w", err)
	}
	// 不再调 s.Get 重走解密链：已知刚加密的明文，直接构造响应。
	// 这样可避免 SecretEncryptor 解密失败时 APIKey 被静默清空的问题（见 Fix 4）。
	m.APIKey = strings.TrimSpace(p.APIKey)
	s.reconcileScheduler(m)
	return m, nil
}

// Duplicate creates an independent, disabled copy of an existing monitor.
// The API key stays server-side: it is decrypted only long enough to encrypt a
// fresh ciphertext for the new row. Runtime state and history are not copied.
func (s *ChannelMonitorService) Duplicate(
	ctx context.Context,
	id, createdBy int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	existing, err := s.RecoverDuplicate(ctx, id, actorScope, operationKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	source, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	plainAPIKey, err := s.decryptAPIKeyForDuplicate(source)
	if err != nil {
		return nil, err
	}
	encryptedAPIKey, err := s.encryptor.Encrypt(plainAPIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt duplicate channel monitor api key: %w", err)
	}
	bodyOverride, err := cloneChannelMonitorJSONMap(source.BodyOverride)
	if err != nil {
		return nil, fmt.Errorf("clone duplicate channel monitor body override: %w", err)
	}

	duplicate := &ChannelMonitor{
		Name:                  duplicateChannelMonitorName(source.Name),
		Provider:              source.Provider,
		APIMode:               source.APIMode,
		Endpoint:              source.Endpoint,
		APIKey:                encryptedAPIKey,
		PrimaryModel:          source.PrimaryModel,
		ExtraModels:           append([]string{}, source.ExtraModels...),
		GroupName:             source.GroupName,
		AccountGroupID:        cloneInt64Pointer(source.AccountGroupID),
		Enabled:               false,
		IntervalSeconds:       source.IntervalSeconds,
		JitterSeconds:         source.JitterSeconds,
		RequestTimeoutSeconds: source.RequestTimeoutSeconds,
		CreatedBy:             createdBy,
		TemplateID:            cloneInt64Pointer(source.TemplateID),
		ExtraHeaders:          cloneChannelMonitorHeaders(source.ExtraHeaders),
		BodyOverrideMode:      source.BodyOverrideMode,
		BodyOverride:          bodyOverride,
		DuplicateOperationID:  operationID,
	}
	if err := s.repo.Create(ctx, duplicate); err != nil {
		return nil, fmt.Errorf("duplicate channel monitor: %w", err)
	}

	// Match Create/Update response semantics: repository receives ciphertext,
	// while handlers receive plaintext only so they can return the masked form.
	duplicate.APIKey = plainAPIKey
	return duplicate, nil
}

// RecoverDuplicate performs a read-only lookup for a duplicate that was
// already committed for the same actor, source monitor, and idempotency key.
// It deliberately never repeats the create side effect.
func (s *ChannelMonitorService) RecoverDuplicate(
	ctx context.Context,
	id int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	if operationID == "" {
		return nil, nil
	}
	monitor, err := s.repo.FindByDuplicateOperationID(ctx, operationID)
	if err != nil {
		return nil, fmt.Errorf("find duplicate channel monitor operation: %w", err)
	}
	if monitor == nil {
		return nil, nil
	}
	s.decryptInPlace(monitor)
	return monitor, nil
}

func duplicateChannelMonitorOperationID(sourceID int64, actorScope, operationKey string) string {
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" {
		return ""
	}
	actorScope = strings.TrimSpace(actorScope)
	if actorScope == "" {
		actorScope = "admin:0"
	}
	payload := "admin.channel_monitors.duplicate\x00" + actorScope + "\x00" + strconv.FormatInt(sourceID, 10) + "\x00" + operationKey
	digest := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", digest)
}

func (s *ChannelMonitorService) decryptAPIKeyForDuplicate(source *ChannelMonitor) (string, error) {
	if source == nil || strings.TrimSpace(source.APIKey) == "" {
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	plain, err := s.encryptor.Decrypt(source.APIKey)
	if err != nil || strings.TrimSpace(plain) == "" {
		slog.Warn("channel_monitor: decrypt api key for duplicate failed",
			"monitor_id", source.ID, "error", err)
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	return plain, nil
}

func duplicateChannelMonitorName(sourceName string) string {
	const suffix = " (Copy)"
	nameRunes := []rune(strings.TrimSpace(sourceName))
	maxBaseRunes := maxChannelMonitorNameRunes - len([]rune(suffix))
	if len(nameRunes) > maxBaseRunes {
		nameRunes = nameRunes[:maxBaseRunes]
	}
	return string(nameRunes) + suffix
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelMonitorHeaders(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneChannelMonitorJSONMap(source map[string]any) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	cloned := make(map[string]any, len(source))
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

// validateCreateParams 把 Create 入参的所有校验聚拢为一个函数，避免 Create 主体超过 30 行。
func validateCreateParams(p ChannelMonitorCreateParams) error {
	if err := validateProvider(p.Provider); err != nil {
		return err
	}
	if err := validateAPIMode(p.Provider, p.APIMode); err != nil {
		return err
	}
	if err := validateInterval(p.IntervalSeconds); err != nil {
		return err
	}
	if err := validateJitter(p.JitterSeconds, p.IntervalSeconds); err != nil {
		return err
	}
	if err := validateRequestTimeout(defaultRequestTimeoutSeconds(p.APIMode, p.RequestTimeoutSeconds)); err != nil {
		return err
	}
	if err := validateEndpoint(p.Endpoint); err != nil {
		return err
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return ErrChannelMonitorMissingAPIKey
	}
	if normalizeMonitorPrimaryModel(p.Provider, p.PrimaryModel) == "" {
		return ErrChannelMonitorMissingPrimaryModel
	}
	return nil
}

// Update 更新监控。APIKey 字段：nil 或空字符串 = 不修改；非空 = 加密后覆盖。
func (s *ChannelMonitorService) Update(ctx context.Context, id int64, p ChannelMonitorUpdateParams) (*ChannelMonitor, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	previousProbeConfig := channelMonitorProbeConfigSnapshot(existing)
	if err := applyMonitorUpdate(existing, p); err != nil {
		return nil, err
	}

	newPlainAPIKey, apiKeyUpdated, err := s.applyAPIKeyUpdate(existing, p.APIKey)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update channel monitor: %w", err)
	}
	if previousProbeConfig.changed(existing) {
		s.clearAdaptiveProbeState(ctx, existing.ID)
	}

	// 不再调 s.Get 重走解密链：避免二次解密带来的"密文被静默清空"风险（与 Create 一致）。
	if apiKeyUpdated {
		existing.APIKey = newPlainAPIKey
	} else {
		s.decryptInPlace(existing)
	}
	s.reconcileScheduler(existing)
	return existing, nil
}

type channelMonitorProbeConfig struct {
	provider        string
	apiMode         string
	primaryModel    string
	extraModelsKey  string
	accountGroupID  int64
	hasAccountGroup bool
}

func channelMonitorProbeConfigSnapshot(m *ChannelMonitor) channelMonitorProbeConfig {
	snapshot := channelMonitorProbeConfig{
		provider:       m.Provider,
		apiMode:        m.APIMode,
		primaryModel:   m.PrimaryModel,
		extraModelsKey: strings.Join(m.ExtraModels, "\x00"),
	}
	if m.AccountGroupID != nil {
		snapshot.accountGroupID = *m.AccountGroupID
		snapshot.hasAccountGroup = true
	}
	return snapshot
}

func (before channelMonitorProbeConfig) changed(after *ChannelMonitor) bool {
	if after == nil || before.provider != after.Provider ||
		before.apiMode != after.APIMode ||
		before.primaryModel != after.PrimaryModel ||
		before.extraModelsKey != strings.Join(after.ExtraModels, "\x00") {
		return true
	}
	if before.hasAccountGroup != (after.AccountGroupID != nil) {
		return true
	}
	return before.hasAccountGroup && before.accountGroupID != *after.AccountGroupID
}

func (s *ChannelMonitorService) clearAdaptiveProbeState(ctx context.Context, monitorID int64) {
	stateRepo, ok := s.repo.(channelMonitorAccountProbeStateRepository)
	if !ok || monitorID <= 0 {
		return
	}
	if err := stateRepo.ClearAccountProbeStates(ctx, monitorID); err != nil {
		slog.Warn("channel_monitor: clear adaptive probe states failed",
			"monitor_id", monitorID, "error", err)
	}
}

// applyAPIKeyUpdate 处理 Update 中的 APIKey 字段：
//   - 入参 raw 为 nil 或空白：不修改 existing.APIKey（仍为密文），返回 updated=false
//   - 非空：加密后写入 existing.APIKey；同时把明文返回给调用方，
//     供写库成功后塞回 existing 避免把密文吐回客户端
func (s *ChannelMonitorService) applyAPIKeyUpdate(existing *ChannelMonitor, raw *string) (plain string, updated bool, err error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", false, nil
	}
	plain = strings.TrimSpace(*raw)
	encrypted, encErr := s.encryptor.Encrypt(plain)
	if encErr != nil {
		return "", false, fmt.Errorf("encrypt api key: %w", encErr)
	}
	existing.APIKey = encrypted
	return plain, true, nil
}

// Delete 删除监控（历史通过外键 CASCADE 自动清理）。
func (s *ChannelMonitorService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel monitor: %w", err)
	}
	s.reconcileScheduler(nil, id)
	return nil
}

// ListHistory 列出某个监控最近的检测历史。
// model 为空表示返回所有模型；limit <= 0 时使用默认值，超过上限会被截断。
func (s *ChannelMonitorService) ListHistory(ctx context.Context, id int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = MonitorHistoryDefaultLimit
	}
	if limit > MonitorHistoryMaxLimit {
		limit = MonitorHistoryMaxLimit
	}
	entries, err := s.repo.ListHistory(ctx, id, strings.TrimSpace(model), limit)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	return entries, nil
}

// ---------- 业务 ----------

// RunCheck 同步触发对一个监控的检测：并发跑 primary + extra 模型，
// 写历史记录并更新 last_checked_at。返回每个模型的检测结果。
func (s *ChannelMonitorService) RunCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	return s.RunCheckWithOptions(ctx, id, false)
}

// RunScheduledCheck is reserved for the background scheduler. Manual checks
// still run a live active probe so administrators can diagnose an upstream
// immediately instead of seeing an older end-user request observation.
func (s *ChannelMonitorService) RunScheduledCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	trafficResults, missingModels, trafficApplicable := s.collectTrafficObservations(ctx, m)
	if trafficApplicable && len(trafficResults) > 0 {
		if s.shouldPersistTrafficObservation(m.ID, trafficResults) {
			s.persistCheckResults(ctx, m, trafficResults, nil)
		}
		if len(missingModels) == 0 {
			return trafficResults, nil
		}
	}
	if m.APIKeyDecryptFailed {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	if len(trafficResults) == 0 {
		return s.runCheckForMonitor(ctx, m, false)
	}

	activeMonitor := monitorWithModels(m, missingModels)
	activeResults, err := s.runCheckForMonitor(ctx, activeMonitor, false)
	return orderCheckResults(uniqueMonitorModels(m), trafficResults, activeResults), err
}

func monitorWithModels(m *ChannelMonitor, models []string) *ChannelMonitor {
	if m == nil || len(models) == 0 {
		return m
	}
	clone := *m
	clone.PrimaryModel = models[0]
	clone.ExtraModels = append([]string(nil), models[1:]...)
	return &clone
}

func orderCheckResults(models []string, resultSets ...[]*CheckResult) []*CheckResult {
	byModel := make(map[string]*CheckResult, len(models))
	for _, results := range resultSets {
		for _, result := range results {
			if result != nil && strings.TrimSpace(result.Model) != "" {
				byModel[result.Model] = result
			}
		}
	}
	ordered := make([]*CheckResult, 0, len(byModel))
	for _, model := range models {
		if result := byModel[model]; result != nil {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

// RunCheckWithOptions synchronously probes one monitor. forceFull is only
// meaningful for monitors explicitly bound to an account-management group.
func (s *ChannelMonitorService) RunCheckWithOptions(ctx context.Context, id int64, forceFull bool) ([]*CheckResult, error) {
	m, err := s.Get(ctx, id) // 已解密 APIKey
	if err != nil {
		return nil, err
	}
	if m.APIKeyDecryptFailed {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	return s.runCheckForMonitor(ctx, m, forceFull)
}

func (s *ChannelMonitorService) runCheckForMonitor(
	ctx context.Context,
	m *ChannelMonitor,
	forceFull bool,
) ([]*CheckResult, error) {
	if results, handled := s.runAdaptiveAccountGroupProbeIfConfigured(ctx, m, forceFull); handled {
		s.persistCheckResults(ctx, m, results, nil)
		return results, nil
	}
	// Explicit account-group bindings never fall back to the legacy
	// GroupName-based OpenAI probe. If adaptive probing is disabled or its
	// dependencies are unavailable, retain the monitor's static behavior.
	if m.AccountGroupID == nil {
		if results, handled := s.runBusinessGroupProbeIfConfigured(ctx, m); handled {
			s.persistCheckResults(ctx, m, results, nil)
			return results, nil
		}
	}
	results, latestImage := s.runChecksConcurrent(ctx, m)
	s.persistCheckResults(ctx, m, results, latestImage)
	return results, nil
}

// RunGroupCheck probes compatible lines from one logical group in parallel.
// The scheduler uses this path; the admin "run now" action intentionally stays
// a one-line diagnostic so an operator can inspect a specific account.
func (s *ChannelMonitorService) RunGroupCheck(ctx context.Context, ids []int64) (*MonitorGroupCheckSummary, error) {
	monitors := s.loadGroupProbeMonitors(ctx, ids)
	if len(monitors) == 0 {
		return nil, ErrChannelMonitorNotFound
	}

	groupKey := monitorProbeGroupKey(monitors[0])
	compatible := make([]*ChannelMonitor, 0, len(monitors))
	for _, m := range monitors {
		if monitorProbeGroupKey(m) == groupKey && defaultAPIMode(m.APIMode) != MonitorAPIModeImages {
			compatible = append(compatible, m)
		}
	}
	compatible = limitMonitorCandidates(compatible)
	if len(compatible) == 0 {
		return nil, ErrChannelMonitorNotFound
	}

	probes := s.runGroupProbeChecks(ctx, compatible)
	for _, probe := range probes {
		s.persistCheckResults(ctx, probe.monitor, []*CheckResult{probe.result}, nil)
	}

	summary := selectBestGroupProbe(probes)
	if summary != nil {
		slog.Info("channel_monitor: grouped probe complete",
			"group", summary.GroupName,
			"candidates", summary.CandidateCount,
			"successful", summary.SuccessfulCount,
			"best_monitor_id", summary.BestMonitorID,
			"best_status", summary.BestStatus,
			"best_latency_ms", summary.BestLatencyMs,
		)
	}
	return summary, nil
}

// RunScheduledGroupCheck preserves the existing all-lines active-probe fallback
// for a logical monitor group. It skips that billable group probe only when
// every compatible line has a fresh real-request observation of its own.
func (s *ChannelMonitorService) RunScheduledGroupCheck(
	ctx context.Context,
	ids []int64,
) (*MonitorGroupCheckSummary, error) {
	monitors := s.loadGroupProbeMonitors(ctx, ids)
	if len(monitors) == 0 {
		return nil, ErrChannelMonitorNotFound
	}

	groupKey := monitorProbeGroupKey(monitors[0])
	compatible := make([]*ChannelMonitor, 0, len(monitors))
	for _, m := range monitors {
		if monitorProbeGroupKey(m) == groupKey && defaultAPIMode(m.APIMode) != MonitorAPIModeImages {
			compatible = append(compatible, m)
		}
	}
	compatible = limitMonitorCandidates(compatible)
	if len(compatible) == 0 {
		return nil, ErrChannelMonitorNotFound
	}

	observed := make([]groupProbeResult, 0, len(compatible))
	for _, monitor := range compatible {
		results, handled := s.runTrafficObservationIfConfigured(ctx, monitor)
		if !handled || len(results) == 0 {
			return s.RunGroupCheck(ctx, ids)
		}
		observed = append(observed, groupProbeResult{
			monitor: monitor,
			result:  results[0],
		})
	}

	for _, probe := range observed {
		if s.shouldPersistTrafficObservation(probe.monitor.ID, []*CheckResult{probe.result}) {
			s.persistCheckResults(ctx, probe.monitor, []*CheckResult{probe.result}, nil)
		}
	}
	return selectBestGroupProbe(observed), nil
}

type groupProbeResult struct {
	monitor *ChannelMonitor
	result  *CheckResult
}

func (s *ChannelMonitorService) loadGroupProbeMonitors(ctx context.Context, ids []int64) []*ChannelMonitor {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]*ChannelMonitor, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		m, err := s.Get(ctx, id)
		if err != nil {
			slog.Warn("channel_monitor: grouped probe skipped missing line",
				"monitor_id", id, "error", err)
			continue
		}
		if !m.Enabled {
			continue
		}
		out = append(out, m)
	}
	return out
}

// runGroupProbeChecks tests only primary models. Extra models are deliberately
// excluded because they would multiply the cost of every candidate probe.
func (s *ChannelMonitorService) runGroupProbeChecks(ctx context.Context, monitors []*ChannelMonitor) []groupProbeResult {
	results := make([]groupProbeResult, len(monitors))
	var eg errgroup.Group
	eg.SetLimit(monitorGroupProbeParallelism)

	for i, monitor := range monitors {
		i, monitor := i, monitor
		eg.Go(func() error {
			results[i] = groupProbeResult{
				monitor: monitor,
				result:  runLowCostPrimaryCheck(ctx, monitor),
			}
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

func runLowCostPrimaryCheck(ctx context.Context, m *ChannelMonitor) *CheckResult {
	if m == nil {
		return &CheckResult{
			Status:    MonitorStatusError,
			Message:   "group probe has no monitor",
			CheckedAt: time.Now(),
		}
	}
	if m.APIKeyDecryptFailed || strings.TrimSpace(m.APIKey) == "" {
		return &CheckResult{
			Model:     m.PrimaryModel,
			Status:    MonitorStatusError,
			Message:   "api key decryption failed; please re-edit the monitor with a fresh key",
			CheckedAt: time.Now(),
		}
	}
	opts := &CheckOptions{
		APIMode:          m.APIMode,
		LowCost:          true,
		ExtraHeaders:     m.ExtraHeaders,
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
	}
	result := runCheckForModel(ctx, m.Provider, m.Endpoint, m.APIKey, m.PrimaryModel, opts)
	result.PingLatencyMs = pingEndpointOrigin(ctx, m.Endpoint)
	return result
}

func selectBestGroupProbe(probes []groupProbeResult) *MonitorGroupCheckSummary {
	if len(probes) == 0 {
		return nil
	}
	var best groupProbeResult
	hasBest := false
	successful := 0
	for _, probe := range probes {
		if probe.monitor == nil || probe.result == nil {
			continue
		}
		if isMonitorHealthyStatus(probe.result.Status) {
			successful++
		}
		if !hasBest || isBetterGroupProbe(probe, best) {
			best = probe
			hasBest = true
		}
	}
	if !hasBest {
		return nil
	}
	return &MonitorGroupCheckSummary{
		GroupName:       monitorGroupDisplayName(best.monitor),
		CandidateCount:  len(probes),
		SuccessfulCount: successful,
		BestMonitorID:   best.monitor.ID,
		BestMonitorName: best.monitor.Name,
		BestStatus:      best.result.Status,
		BestLatencyMs:   best.result.LatencyMs,
	}
}

func isBetterGroupProbe(candidate, incumbent groupProbeResult) bool {
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
	return candidate.monitor.ID < incumbent.monitor.ID
}

func isMonitorHealthyStatus(status string) bool {
	return status == MonitorStatusOperational || status == MonitorStatusDegraded
}

func monitorStatusRank(status string) int {
	switch status {
	case MonitorStatusOperational:
		return 4
	case MonitorStatusDegraded:
		return 3
	case MonitorStatusFailed:
		return 2
	case MonitorStatusError:
		return 1
	default:
		return 0
	}
}

func monitorLatencySortValue(latency *int) int {
	if latency == nil {
		return int(^uint(0) >> 1)
	}
	return *latency
}

// persistCheckResults 写入本次检测的历史记录并更新 last_checked_at。
// 任一写库失败都只记日志，不影响调用方拿到 results（与 MVP 期望一致：宁可漏记历史也要先返回结果）。
func (s *ChannelMonitorService) persistCheckResults(
	ctx context.Context,
	m *ChannelMonitor,
	results []*CheckResult,
	latestImage *monitorLatestImagePayload,
) {
	persistCtx, cancel := monitorPersistenceContext(ctx)
	defer cancel()

	for _, result := range results {
		s.recordMonitorCost(persistCtx, m, result, nil)
	}
	rows := make([]*ChannelMonitorHistoryRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, &ChannelMonitorHistoryRow{
			MonitorID:      m.ID,
			Model:          r.Model,
			Status:         r.Status,
			LatencyMs:      r.LatencyMs,
			PingLatencyMs:  r.PingLatencyMs,
			AccountID:      r.AccountID,
			AccountName:    r.AccountName,
			ProbeMode:      r.ProbeMode,
			CandidateCount: r.CandidateCount,
			HealthyCount:   r.HealthyCount,
			Message:        r.Message,
			CheckedAt:      r.CheckedAt,
		})
	}
	if err := s.repo.InsertHistoryBatch(persistCtx, rows); err != nil {
		slog.Error("channel_monitor: insert history failed",
			"monitor_id", m.ID, "name", m.Name, "error", err)
	}
	if err := s.repo.MarkChecked(persistCtx, m.ID, time.Now()); err != nil {
		slog.Error("channel_monitor: mark checked failed",
			"monitor_id", m.ID, "error", err)
	}
	s.persistLatestImage(persistCtx, m.ID, latestImage)
}

func monitorPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), monitorPersistenceTimeout)
}

// runChecksConcurrent 对 primary + extra 模型并发执行检测。
// errgroup 仅用于等待，不传播错误（每个 model 失败都已打包进 CheckResult）。
func (s *ChannelMonitorService) runChecksConcurrent(ctx context.Context, m *ChannelMonitor) ([]*CheckResult, *monitorLatestImagePayload) {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	// 所有模型共用同一份 CheckOptions（来自监控的快照字段）。
	opts := &CheckOptions{
		APIMode:          m.APIMode,
		RequestTimeout:   monitorRequestTimeoutFor(m),
		ExtraHeaders:     m.ExtraHeaders,
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
	}
	if checkAPIMode(opts) == MonitorAPIModeImages {
		// A real image generation is intentionally limited to the primary model:
		// extra models would multiply upstream image cost on every scheduled run.
		result, image := runImageCheckForModel(ctx, m.Endpoint, m.APIKey, m.PrimaryModel, opts)
		result.PingLatencyMs = pingEndpointOrigin(ctx, m.Endpoint)
		return []*CheckResult{result}, image
	}

	results := make([]*CheckResult, len(models))

	// ping 共享一次，所有模型记录同一个 ping 延迟。
	pingMs := pingEndpointOrigin(ctx, m.Endpoint)

	var eg errgroup.Group
	var mu sync.Mutex
	for i, model := range models {
		i, model := i, model
		eg.Go(func() error {
			r := runCheckForModel(ctx, m.Provider, m.Endpoint, m.APIKey, model, opts)
			r.PingLatencyMs = pingMs
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return results, nil
}

func (s *ChannelMonitorService) persistLatestImage(
	ctx context.Context,
	monitorID int64,
	payload *monitorLatestImagePayload,
) {
	if payload == nil {
		return
	}
	repo, ok := s.repo.(ChannelMonitorLatestImageRepository)
	if !ok {
		slog.Warn("channel_monitor: latest image repository unavailable", "monitor_id", monitorID)
		return
	}
	image := &ChannelMonitorLatestImage{
		MonitorID:   monitorID,
		ContentType: payload.ContentType,
		Data:        append([]byte(nil), payload.Data...),
		GeneratedAt: time.Now().UTC(),
	}
	if err := repo.UpsertLatestImage(ctx, image); err != nil {
		slog.Error("channel_monitor: upsert latest image failed",
			"monitor_id", monitorID, "error", err)
	}
}

// GetLatestImage returns the most recent successful image for an existing
// monitor. Failed checks never replace the previous image.
func (s *ChannelMonitorService) GetLatestImage(ctx context.Context, monitorID int64) (*ChannelMonitorLatestImage, error) {
	if _, err := s.repo.GetByID(ctx, monitorID); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(ChannelMonitorLatestImageRepository)
	if !ok {
		return nil, ErrChannelMonitorLatestImageNotFound
	}
	image, err := repo.GetLatestImage(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	return image, nil
}

// GetLatestImageForUser returns the latest image only for an enabled monitor.
// User-facing routes must not expose images from disabled monitors.
func (s *ChannelMonitorService) GetLatestImageForUser(ctx context.Context, monitorID int64) (*ChannelMonitorLatestImage, error) {
	monitor, err := s.repo.GetByID(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	if !monitor.Enabled {
		return nil, ErrChannelMonitorNotFound
	}
	repo, ok := s.repo.(ChannelMonitorLatestImageRepository)
	if !ok {
		return nil, ErrChannelMonitorLatestImageNotFound
	}
	image, err := repo.GetLatestImage(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	return image, nil
}

// ---------- 调度器协作 ----------

// SetScheduler 由 wire 在 runner 构造后注入，用于在 CRUD 时即时同步任务表。
// 通过 setter 注入避免 service ↔ runner 的依赖环。
func (s *ChannelMonitorService) SetScheduler(sched MonitorScheduler) {
	s.scheduler = sched
}

// reconcileScheduler keeps grouped tasks correct after any CRUD operation.
// Legacy test doubles retain the old Schedule/Unschedule callbacks, while the
// real runner reloads all enabled rows so moved or renamed lines cannot leave a
// stale group task behind.
func (s *ChannelMonitorService) reconcileScheduler(m *ChannelMonitor, deletedID ...int64) {
	if s.scheduler == nil {
		return
	}
	if reconciler, ok := s.scheduler.(interface{ Reconcile() }); ok {
		reconciler.Reconcile()
		return
	}
	if m != nil {
		s.scheduler.Schedule(m)
		return
	}
	if len(deletedID) > 0 {
		s.scheduler.Unschedule(deletedID[0])
	}
}

// ListEnabledMonitors 返回所有 enabled=true 的监控（解密后），供 runner 启动时建立任务表。
func (s *ChannelMonitorService) ListEnabledMonitors(ctx context.Context) ([]*ChannelMonitor, error) {
	all, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		s.decryptInPlace(m)
	}
	return all, nil
}

// cleanupOldHistory 删除 monitorHistoryRetentionDays 天之前的明细历史记录。
// 由 RunDailyMaintenance 调用；SoftDeleteMixin 自动把 DELETE 改为 UPDATE deleted_at。
func (s *ChannelMonitorService) cleanupOldHistory(ctx context.Context) error {
	before := time.Now().UTC().AddDate(0, 0, -monitorHistoryRetentionDays)
	deleted, err := s.repo.DeleteHistoryBefore(ctx, before)
	if err != nil {
		return fmt.Errorf("delete history before %s: %w", before.Format(time.RFC3339), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: history cleanup",
			"deleted_rows", deleted, "before", before.Format(time.RFC3339))
	}
	return nil
}

// RunDailyMaintenance 每日维护任务：聚合昨天之前未聚合的明细，软删过期明细和聚合。
// 由 OpsCleanupService 的 cron 调度触发（共享 schedule 和 leader lock）。
//
// 幂等性：
//   - watermark 保证已聚合的日期不会重复处理；
//   - UpsertDailyRollupsFor 内部使用 ON CONFLICT DO UPDATE，同一日重复跑结果一致。
//
// 每一步失败都只记 slog.Warn，整体函数始终返回 nil 让后续步骤能继续跑
// （与 OpsCleanupService.runCleanupOnce 风格一致）。
func (s *ChannelMonitorService) RunDailyMaintenance(ctx context.Context) error {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	if err := s.runDailyAggregation(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "aggregate", "error", err)
	}
	if err := s.cleanupOldHistory(ctx); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_history", "error", err)
	}
	if err := s.cleanupOldRollups(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_rollups", "error", err)
	}
	return nil
}

// runDailyAggregation 从 watermark+1 聚合到昨天（UTC）。
// 首次跑（watermark nil）：从 today-monitorRollupRetentionDays 开始回填。
// 每次最多聚合 monitorMaintenanceMaxDaysPerRun 天，避免长事务。
func (s *ChannelMonitorService) runDailyAggregation(ctx context.Context, today time.Time) error {
	watermark, err := s.repo.LoadAggregationWatermark(ctx)
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}

	start := s.resolveAggregationStart(watermark, today)
	if !start.Before(today) {
		return nil // 没有需要聚合的日期
	}

	iterations := 0
	for d := start; d.Before(today); d = d.Add(24 * time.Hour) {
		if iterations >= monitorMaintenanceMaxDaysPerRun {
			slog.Info("channel_monitor: maintenance aggregation capped",
				"max_days", monitorMaintenanceMaxDaysPerRun,
				"next_resume", d.Format("2006-01-02"))
			break
		}
		affected, upErr := s.repo.UpsertDailyRollupsFor(ctx, d)
		if upErr != nil {
			return fmt.Errorf("upsert rollups for %s: %w", d.Format("2006-01-02"), upErr)
		}
		if err := s.repo.UpdateAggregationWatermark(ctx, d); err != nil {
			return fmt.Errorf("update watermark to %s: %w", d.Format("2006-01-02"), err)
		}
		slog.Info("channel_monitor: rollups upserted",
			"date", d.Format("2006-01-02"), "affected_rows", affected)
		iterations++
	}
	return nil
}

// resolveAggregationStart 计算本次聚合起点：
//   - watermark == nil：today - monitorRollupRetentionDays（首次回填最多 30 天）
//   - watermark != nil：*watermark + 1 day
func (s *ChannelMonitorService) resolveAggregationStart(watermark *time.Time, today time.Time) time.Time {
	if watermark == nil {
		return today.AddDate(0, 0, -monitorRollupRetentionDays)
	}
	return watermark.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// cleanupOldRollups 软删 bucket_date < today - monitorRollupRetentionDays 的日聚合行。
func (s *ChannelMonitorService) cleanupOldRollups(ctx context.Context, today time.Time) error {
	cutoff := today.AddDate(0, 0, -monitorRollupRetentionDays)
	deleted, err := s.repo.DeleteRollupsBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete rollups before %s: %w", cutoff.Format("2006-01-02"), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: rollups cleanup",
			"deleted_rows", deleted, "before", cutoff.Format("2006-01-02"))
	}
	return nil
}

// ---------- helpers ----------

// decryptInPlace 把 ChannelMonitor.APIKey 从密文解密为明文。
// 解密失败时把字段清空 + 设置 APIKeyDecryptFailed=true（不返回错误，避免阻断列表渲染）。
// runner / RunCheck 必须读取该标志位并拒绝执行检测。
func (s *ChannelMonitorService) decryptInPlace(m *ChannelMonitor) {
	if m == nil || m.APIKey == "" {
		return
	}
	plain, err := s.encryptor.Decrypt(m.APIKey)
	if err != nil {
		slog.Warn("channel_monitor: decrypt api key failed",
			"monitor_id", m.ID, "error", err)
		m.APIKey = ""
		m.APIKeyDecryptFailed = true
		return
	}
	m.APIKey = plain
}

// applyMonitorUpdate 把 update params 中非 nil 的字段应用到 existing 上。
// APIKey 字段在调用方单独处理（涉及加密）。
//
// 行数稍超过 30：这是逐字段平铺的 dispatcher，每个 if 都是 1-3 行的"非 nil 则覆盖"模式，
// 拆分反而会增加跳转噪音、影响可读性，故保留为单函数。
func applyMonitorUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams) error {
	providerChanged := false
	if p.Name != nil {
		existing.Name = strings.TrimSpace(*p.Name)
	}
	if p.Provider != nil {
		if err := validateProvider(*p.Provider); err != nil {
			return err
		}
		providerChanged = existing.Provider != *p.Provider
		existing.Provider = *p.Provider
	}
	if p.Endpoint != nil {
		if err := validateEndpoint(*p.Endpoint); err != nil {
			return err
		}
		existing.Endpoint = normalizeEndpoint(*p.Endpoint)
	}
	if p.PrimaryModel != nil {
		primaryModel := normalizeMonitorPrimaryModel(existing.Provider, *p.PrimaryModel)
		if primaryModel == "" {
			return ErrChannelMonitorMissingPrimaryModel
		}
		existing.PrimaryModel = primaryModel
	} else if providerChanged && existing.Provider == MonitorProviderGrok {
		existing.PrimaryModel = MonitorDefaultGrokModel
	}
	if p.ExtraModels != nil {
		existing.ExtraModels = normalizeModels(*p.ExtraModels)
	}
	if p.GroupName != nil {
		existing.GroupName = strings.TrimSpace(*p.GroupName)
	}
	if p.ClearAccountGroup {
		existing.AccountGroupID = nil
	} else if p.AccountGroupID != nil {
		existing.AccountGroupID = cloneInt64Pointer(p.AccountGroupID)
	}
	if p.Enabled != nil {
		existing.Enabled = *p.Enabled
	}
	if p.IntervalSeconds != nil {
		if err := validateInterval(*p.IntervalSeconds); err != nil {
			return err
		}
		existing.IntervalSeconds = *p.IntervalSeconds
	}
	if p.JitterSeconds != nil {
		existing.JitterSeconds = *p.JitterSeconds
	}
	if p.RequestTimeoutSeconds != nil {
		if err := validateRequestTimeout(*p.RequestTimeoutSeconds); err != nil {
			return err
		}
		existing.RequestTimeoutSeconds = *p.RequestTimeoutSeconds
	}
	if p.IntervalSeconds != nil || p.JitterSeconds != nil {
		// interval 与 jitter 任一变化都需要重新校验组合约束（interval - jitter >= 下限）。
		if err := validateJitter(existing.JitterSeconds, existing.IntervalSeconds); err != nil {
			return err
		}
	}
	return applyMonitorAdvancedUpdate(existing, p, providerChanged)
}

// applyMonitorAdvancedUpdate 处理自定义请求快照相关字段，从 applyMonitorUpdate 拆出避免过长。
func applyMonitorAdvancedUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams, providerChanged bool) error {
	if p.ClearTemplate {
		existing.TemplateID = nil
	} else if p.TemplateID != nil {
		id := *p.TemplateID
		existing.TemplateID = &id
	}
	if p.ExtraHeaders != nil {
		if err := validateExtraHeaders(*p.ExtraHeaders); err != nil {
			return err
		}
		existing.ExtraHeaders = emptyHeadersIfNil(*p.ExtraHeaders)
	}
	newAPIMode := defaultAPIMode(existing.APIMode)
	previousAPIMode := newAPIMode
	if p.APIMode != nil {
		newAPIMode = defaultAPIMode(*p.APIMode)
	} else if existing.Provider != MonitorProviderOpenAI {
		newAPIMode = MonitorAPIModeChatCompletions
	}
	if err := validateAPIMode(existing.Provider, newAPIMode); err != nil {
		return err
	}
	// BodyOverrideMode / BodyOverride 联合校验，和模板一致。
	newMode := existing.BodyOverrideMode
	newBody := existing.BodyOverride
	if p.BodyOverrideMode != nil {
		newMode = *p.BodyOverrideMode
	}
	if p.BodyOverride != nil {
		newBody = *p.BodyOverride
	}
	if providerChanged || p.APIMode != nil || p.BodyOverrideMode != nil || p.BodyOverride != nil {
		if err := validateBodyModeForProtocol(existing.Provider, newAPIMode, newMode, newBody); err != nil {
			return err
		}
		existing.BodyOverrideMode = defaultBodyMode(newMode)
		existing.BodyOverride = newBody
	}
	if p.APIMode != nil && p.RequestTimeoutSeconds == nil &&
		existing.RequestTimeoutSeconds == defaultRequestTimeoutSeconds(previousAPIMode, 0) {
		existing.RequestTimeoutSeconds = defaultRequestTimeoutSeconds(newAPIMode, 0)
	}
	existing.APIMode = newAPIMode
	return nil
}
