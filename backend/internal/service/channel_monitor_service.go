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

// ChannelMonitorRepository ???????????
// ??/?????????? service ?? ChannelMonitor ???
// repository ????? ent ???????? api_key_encrypted ??????
type ChannelMonitorRepository interface {
	// CRUD
	Create(ctx context.Context, m *ChannelMonitor) error
	GetByID(ctx context.Context, id int64) (*ChannelMonitor, error)
	Update(ctx context.Context, m *ChannelMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error)
	FindByDuplicateOperationID(ctx context.Context, operationID string) (*ChannelMonitor, error)

	// ?????
	ListEnabled(ctx context.Context) ([]*ChannelMonitor, error)
	MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error
	InsertHistoryBatch(ctx context.Context, rows []*ChannelMonitorHistoryRow) error
	DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error)

	// ????
	ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error)

	// ??????
	ListLatestPerModel(ctx context.Context, monitorID int64) ([]*ChannelMonitorLatest, error)
	ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*ChannelMonitorAvailability, error)

	// ?????admin/user list ???? N+1?
	ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*ChannelMonitorLatest, error)
	ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*ChannelMonitorAvailability, error)
	// ListRecentHistoryForMonitors ????? monitor ??????primaryModels[monitorID]??? perMonitorLimit ????
	// ??? entry ?? checked_at DESC ??????????? message ???
	ListRecentHistoryForMonitors(ctx context.Context, ids []int64, primaryModels map[int64]string, perMonitorLimit int) (map[int64][]*ChannelMonitorHistoryEntry, error)

	// ---------- ?????OpsCleanupService ??? ----------

	// UpsertDailyRollupsFor ? targetDate ?????? (monitor_id, model, bucket_date)
	// ??? channel_monitor_daily_rollups?targetDate ????????
	// ? ON CONFLICT DO UPDATE ????????? upsert ??????
	UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error)
	// DeleteRollupsBefore ?? bucket_date < beforeDate ????????????
	DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error)
	// LoadAggregationWatermark ? watermark?id=1??
	// ?? nil ????????watermark ???????????migration 110 ????
	LoadAggregationWatermark(ctx context.Context) (*time.Time, error)
	// UpdateAggregationWatermark ? watermark?UPSERT ? id=1??
	UpdateAggregationWatermark(ctx context.Context, date time.Time) error
}

// channelMonitorRuntimeReader is the optional settings view used by the
// passive V2 aggregator and the legacy active monitor.
type channelMonitorRuntimeReader interface {
	GetChannelMonitorRuntime(ctx context.Context) ChannelMonitorRuntime
}

// ChannelMonitorService ?????????
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
	settings                     channelMonitorRuntimeReader
	trafficObservationMu         sync.Mutex
	trafficObservationWrites     map[string]time.Time
	// scheduler ? wire ?? SetScheduler ???CRUD ??????????????
	// ??????????? nil????????? no-op?
	scheduler              MonitorScheduler
	internalAPIKeyResolver channelMonitorInternalAPIKeyResolver
	internalGatewayURL     string
}

type channelMonitorInternalAPIKeyResolver interface {
	GetByID(ctx context.Context, id int64) (*APIKey, error)
}

// SetInternalGatewayDependencies configures the optional in-process gateway
// path used by station-owned monitoring keys.
func (s *ChannelMonitorService) SetInternalGatewayDependencies(
	resolver channelMonitorInternalAPIKeyResolver,
	gatewayURL string,
) {
	if s == nil {
		return
	}
	s.internalAPIKeyResolver = resolver
	s.internalGatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
}

const maxChannelMonitorNameRunes = 100

// ChannelMonitorDuplicateOperationIDMetadataKey is stored in the existing
// extra_headers JSON column to avoid a schema migration. The colon makes it an
// invalid HTTP header name, and repository adapters remove it before exposing
// ExtraHeaders to the service layer.
const ChannelMonitorDuplicateOperationIDMetadataKey = "sub2api:duplicate_operation_id"

// NewChannelMonitorService ???????????
func NewChannelMonitorService(repo ChannelMonitorRepository, encryptor SecretEncryptor) *ChannelMonitorService {
	return &ChannelMonitorService{
		repo:                     repo,
		encryptor:                encryptor,
		trafficObservationWrites: make(map[string]time.Time),
	}
}

// SetRuntimeReader injects the settings view used by the optional passive V2
// aggregation layer. It does not disable the existing active monitor.
func (s *ChannelMonitorService) SetRuntimeReader(r channelMonitorRuntimeReader) {
	if s != nil {
		s.settings = r
	}
}

func (s *ChannelMonitorService) probeRuntime(ctx context.Context) ChannelMonitorRuntime {
	if s == nil {
		return ChannelMonitorRuntime{Enabled: true}
	}
	if s.settings != nil {
		return s.settings.GetChannelMonitorRuntime(ctx)
	}
	if s.settingService != nil {
		return s.settingService.GetChannelMonitorRuntime(ctx)
	}
	return ChannelMonitorRuntime{Enabled: true}
}

// ---------- CRUD ----------

// List ??????? provider/enabled/search ?? + ????
// ??? ChannelMonitor.APIKey ???????handler ??????
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

// Get ????????? API Key??
func (s *ChannelMonitorService) Get(ctx context.Context, id int64) (*ChannelMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.decryptInPlace(m)
	return m, nil
}

// Create ????????? api_key??
func (s *ChannelMonitorService) Create(ctx context.Context, p ChannelMonitorCreateParams) (*ChannelMonitor, error) {
	p.SourceMode = normalizeMonitorSourceMode(p.SourceMode)
	if p.SourceMode == MonitorSourceInternalGateway && strings.TrimSpace(p.Endpoint) == "" {
		p.Endpoint = "http://127.0.0.1:8080"
	}
	if p.SourceMode == MonitorSourceInternalGateway && strings.TrimSpace(s.internalGatewayURL) != "" {
		p.Endpoint = s.internalGatewayURL
	}
	if err := validateCreateParams(p); err != nil {
		return nil, err
	}
	if err := validateMonitorSourceParams(p.SourceMode, p.APIKey, p.InternalAPIKeyID, p.InternalGroupID); err != nil {
		return nil, err
	}
	if p.SourceMode == MonitorSourceInternalGateway {
		p.APIKey = internalMonitorAPIKeyMarker
		if strings.TrimSpace(s.internalGatewayURL) != "" {
			p.Endpoint = s.internalGatewayURL
		}
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
		SourceMode:            p.SourceMode,
		InternalAPIKeyID:      cloneInt64Pointer(p.InternalAPIKeyID),
		InternalGroupID:       cloneInt64Pointer(p.InternalGroupID),
		APIKey:                encrypted, // ????? repository ???????
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
	if p.SourceMode == MonitorSourceInternalGateway {
		if err := s.validateInternalMonitorBinding(ctx, m); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create channel monitor: %w", err)
	}
	// ??? s.Get ??????????????????????
	// ????? SecretEncryptor ????? APIKey ?????????? Fix 4??
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

	plainAPIKey := internalMonitorAPIKeyMarker
	if normalizeMonitorSourceMode(source.SourceMode) != MonitorSourceInternalGateway {
		plainAPIKey, err = s.decryptAPIKeyForDuplicate(source)
		if err != nil {
			return nil, err
		}
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
		SourceMode:            normalizeMonitorSourceMode(source.SourceMode),
		InternalAPIKeyID:      cloneInt64Pointer(source.InternalAPIKeyID),
		InternalGroupID:       cloneInt64Pointer(source.InternalGroupID),
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

// validateCreateParams ? Create ????????????????? Create ???? 30 ??
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
	if normalizeMonitorSourceMode(p.SourceMode) == MonitorSourceInternalGateway {
		if defaultAPIMode(p.APIMode) == MonitorAPIModeImages {
			return fmt.Errorf("internal gateway monitoring does not support image probes")
		}
		if _, err := internalMonitorGatewayURL(p.Endpoint); err != nil {
			return err
		}
	} else {
		if err := validateEndpoint(p.Endpoint); err != nil {
			return err
		}
		if strings.TrimSpace(p.APIKey) == "" {
			return ErrChannelMonitorMissingAPIKey
		}
	}
	if normalizeMonitorPrimaryModel(p.Provider, p.PrimaryModel) == "" {
		return ErrChannelMonitorMissingPrimaryModel
	}
	return nil
}

// Update ?????APIKey ???nil ????? = ?????? = ??????
func (s *ChannelMonitorService) Update(ctx context.Context, id int64, p ChannelMonitorUpdateParams) (*ChannelMonitor, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	previousSourceMode := normalizeMonitorSourceMode(existing.SourceMode)
	previousProbeConfig := channelMonitorProbeConfigSnapshot(existing)
	if err := applyMonitorUpdate(existing, p); err != nil {
		return nil, err
	}

	newPlainAPIKey, apiKeyUpdated, err := s.applyAPIKeyUpdate(existing, p.APIKey)
	if err != nil {
		return nil, err
	}
	if normalizeMonitorSourceMode(existing.SourceMode) == MonitorSourceInternalGateway {
		encryptedMarker, encErr := s.encryptor.Encrypt(internalMonitorAPIKeyMarker)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt internal monitor marker: %w", encErr)
		}
		existing.APIKey = encryptedMarker
		if defaultAPIMode(existing.APIMode) == MonitorAPIModeImages {
			return nil, fmt.Errorf("internal gateway monitoring does not support image probes")
		}
		if strings.TrimSpace(s.internalGatewayURL) != "" {
			existing.Endpoint = normalizeEndpoint(s.internalGatewayURL)
		}
		if err := s.validateInternalMonitorBinding(ctx, existing); err != nil {
			return nil, err
		}
	} else if previousSourceMode == MonitorSourceInternalGateway &&
		(p.APIKey == nil || strings.TrimSpace(*p.APIKey) == "") {
		return nil, ErrChannelMonitorMissingAPIKey
	} else if err := validateEndpoint(existing.Endpoint); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update channel monitor: %w", err)
	}
	if previousProbeConfig.changed(existing) {
		s.clearAdaptiveProbeState(ctx, existing.ID)
	}

	// ??? s.Get ???????????????"???????"???? Create ????
	if apiKeyUpdated {
		existing.APIKey = newPlainAPIKey
	} else {
		s.decryptInPlace(existing)
	}
	s.reconcileScheduler(existing)
	return existing, nil
}

type channelMonitorProbeConfig struct {
	provider         string
	apiMode          string
	sourceMode       string
	internalKeyID    int64
	hasInternalKey   bool
	internalGroupID  int64
	hasInternalGroup bool
	primaryModel     string
	extraModelsKey   string
	accountGroupID   int64
	hasAccountGroup  bool
}

func channelMonitorProbeConfigSnapshot(m *ChannelMonitor) channelMonitorProbeConfig {
	snapshot := channelMonitorProbeConfig{
		provider:       m.Provider,
		apiMode:        m.APIMode,
		sourceMode:     normalizeMonitorSourceMode(m.SourceMode),
		primaryModel:   m.PrimaryModel,
		extraModelsKey: strings.Join(m.ExtraModels, "\x00"),
	}
	if m.InternalAPIKeyID != nil {
		snapshot.internalKeyID = *m.InternalAPIKeyID
		snapshot.hasInternalKey = true
	}
	if m.InternalGroupID != nil {
		snapshot.internalGroupID = *m.InternalGroupID
		snapshot.hasInternalGroup = true
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
		before.sourceMode != normalizeMonitorSourceMode(after.SourceMode) ||
		before.primaryModel != after.PrimaryModel ||
		before.extraModelsKey != strings.Join(after.ExtraModels, "\x00") {
		return true
	}
	if before.hasInternalKey != (after.InternalAPIKeyID != nil) ||
		(before.hasInternalKey && before.internalKeyID != *after.InternalAPIKeyID) ||
		before.hasInternalGroup != (after.InternalGroupID != nil) ||
		(before.hasInternalGroup && before.internalGroupID != *after.InternalGroupID) {
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

// applyAPIKeyUpdate ?? Update ?? APIKey ???
//   - ?? raw ? nil ??????? existing.APIKey????????? updated=false
//   - ???????? existing.APIKey?????????????
//     ???????? existing ??????????
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

func (s *ChannelMonitorService) validateInternalMonitorBinding(ctx context.Context, m *ChannelMonitor) error {
	if m == nil || normalizeMonitorSourceMode(m.SourceMode) != MonitorSourceInternalGateway {
		return nil
	}
	if m.InternalAPIKeyID == nil || *m.InternalAPIKeyID <= 0 ||
		m.InternalGroupID == nil || *m.InternalGroupID <= 0 {
		return fmt.Errorf("internal gateway monitor requires an internal API key and group")
	}
	if _, err := s.resolveInternalMonitorKey(ctx, m); err != nil {
		return err
	}
	if strings.TrimSpace(s.internalGatewayURL) != "" {
		m.Endpoint = normalizeEndpoint(s.internalGatewayURL)
	}
	return nil
}

// Delete ??????????? CASCADE ??????
func (s *ChannelMonitorService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel monitor: %w", err)
	}
	s.reconcileScheduler(nil, id)
	return nil
}

// ListHistory ??????????????
// model ???????????limit <= 0 ????????????????
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

// ---------- ?? ----------

// RunCheck ???????????????? primary + extra ???
// ???????? last_checked_at?????????????
func (s *ChannelMonitorService) RunCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	return s.RunCheckWithOptions(ctx, id, false)
}

// RunScheduledCheck is reserved for the background scheduler. Manual checks
// still run a live active probe so administrators can diagnose an upstream
// immediately instead of seeing an older end-user request observation.
func (s *ChannelMonitorService) RunScheduledCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	if !s.probeRuntime(ctx).ActiveProbesAllowed() {
		return nil, ErrChannelMonitorDisabled
	}
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
	m, err := s.Get(ctx, id) // ??? APIKey
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
	if normalizeMonitorSourceMode(m.SourceMode) == MonitorSourceInternalGateway {
		results := s.runInternalGatewayChecks(ctx, m)
		s.persistCheckResults(ctx, m, results, nil)
		return results, nil
	}
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

// persistCheckResults ?????????????? last_checked_at?
// ???????????????????? results?? MVP ????????????????????
func (s *ChannelMonitorService) persistCheckResults(
	ctx context.Context,
	m *ChannelMonitor,
	results []*CheckResult,
	latestImage *monitorLatestImagePayload,
) {
	persistCtx, cancel := monitorPersistenceContext(ctx)
	defer cancel()

	if latestImage == nil {
		for _, result := range results {
			if result != nil && result.monitorLatestImage != nil {
				latestImage = result.monitorLatestImage
				break
			}
		}
	}
	for _, result := range results {
		s.recordMonitorCost(persistCtx, m, result, nil)
	}
	rows := make([]*ChannelMonitorHistoryRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, &ChannelMonitorHistoryRow{
			MonitorID:        m.ID,
			Model:            r.Model,
			Status:           r.Status,
			LatencyMs:        r.LatencyMs,
			LatencyBreakdown: r.LatencyBreakdown.Clone(),
			PingLatencyMs:    r.PingLatencyMs,
			AccountID:        r.AccountID,
			AccountName:      r.AccountName,
			ProbeMode:        r.ProbeMode,
			CandidateCount:   r.CandidateCount,
			HealthyCount:     r.HealthyCount,
			Message:          r.Message,
			CheckedAt:        r.CheckedAt,
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

// runChecksConcurrent ? primary + extra ?????????
// errgroup ?????????????? model ??????? CheckResult??
func (s *ChannelMonitorService) runChecksConcurrent(ctx context.Context, m *ChannelMonitor) ([]*CheckResult, *monitorLatestImagePayload) {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	// ????????? CheckOptions????????????
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

	// ping ?????????????? ping ???
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

// ---------- ????? ----------

// SetScheduler ? wire ? runner ????????? CRUD ?????????
// ?? setter ???? service ? runner ?????
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

// ListEnabledMonitors ???? enabled=true ?????????? runner ?????????
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

// cleanupOldHistory ?? monitorHistoryRetentionDays ???????????
// ? RunDailyMaintenance ???SoftDeleteMixin ??? DELETE ?? UPDATE deleted_at?
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

// RunDailyMaintenance ??????????????????????????????
// ? OpsCleanupService ? cron ??????? schedule ? leader lock??
//
// ????
//   - watermark ???????????????
//   - UpsertDailyRollupsFor ???? ON CONFLICT DO UPDATE????????????
//
// ???????? slog.Warn????????? nil ?????????
// ?? OpsCleanupService.runCleanupOnce ??????
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

// runDailyAggregation ? watermark+1 ??????UTC??
// ????watermark nil??? today-monitorRollupRetentionDays ?????
// ?????? monitorMaintenanceMaxDaysPerRun ????????
func (s *ChannelMonitorService) runDailyAggregation(ctx context.Context, today time.Time) error {
	watermark, err := s.repo.LoadAggregationWatermark(ctx)
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}

	start := s.resolveAggregationStart(watermark, today)
	if !start.Before(today) {
		return nil // ?????????
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

// resolveAggregationStart ?????????
//   - watermark == nil?today - monitorRollupRetentionDays??????? 30 ??
//   - watermark != nil?*watermark + 1 day
func (s *ChannelMonitorService) resolveAggregationStart(watermark *time.Time, today time.Time) time.Time {
	if watermark == nil {
		return today.AddDate(0, 0, -monitorRollupRetentionDays)
	}
	return watermark.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// cleanupOldRollups ?? bucket_date < today - monitorRollupRetentionDays ??????
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

// decryptInPlace ? ChannelMonitor.APIKey ?????????
// ?????????? + ?? APIKeyDecryptFailed=true?????????????????
// runner / RunCheck ????????????????
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

// applyMonitorUpdate ? update params ?? nil ?????? existing ??
// APIKey ?????????????????
//
// ????? 30????????? dispatcher??? if ?? 1-3 ??"? nil ???"???
// ??????????????????????????
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
	if p.SourceMode != nil {
		existing.SourceMode = normalizeMonitorSourceMode(*p.SourceMode)
	}
	if p.InternalAPIKeyID != nil {
		existing.InternalAPIKeyID = cloneInt64Pointer(p.InternalAPIKeyID)
	}
	if p.InternalGroupID != nil {
		existing.InternalGroupID = cloneInt64Pointer(p.InternalGroupID)
	}
	if p.Endpoint != nil {
		if normalizeMonitorSourceMode(existing.SourceMode) != MonitorSourceInternalGateway {
			if err := validateEndpoint(*p.Endpoint); err != nil {
				return err
			}
			existing.Endpoint = normalizeEndpoint(*p.Endpoint)
		}
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
		// interval ? jitter ????????????????interval - jitter >= ????
		if err := validateJitter(existing.JitterSeconds, existing.IntervalSeconds); err != nil {
			return err
		}
	}
	return applyMonitorAdvancedUpdate(existing, p, providerChanged)
}

// applyMonitorAdvancedUpdate ??????????????? applyMonitorUpdate ???????
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
	// BodyOverrideMode / BodyOverride ???????????
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
