package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	// These values live in accounts.extra (non-secret operational flags) so the
	// feature ships without touching the accounts schema.
	UpstreamRatioSyncExtraKey        = "upstream_ratio_sync"         // latest sync status snapshot
	UpstreamRatioSyncEnabledExtraKey = "upstream_ratio_sync_enabled" // per-account switch
	UpstreamRatioSyncGroupExtraKey   = "upstream_ratio_sync_group"   // new-api group name for group_ratio

	// UpstreamRatioSyncPanelTokenCredentialKey stores the optional new-api
	// system access token inside credentials (secret) for upstreams whose
	// /api/pricing requires authentication (some forks gate it behind login).
	UpstreamRatioSyncPanelTokenCredentialKey = "upstream_panel_token"

	upstreamRatioSyncDefaultIntervalMinutes = 30
	upstreamRatioSyncMinIntervalMinutes     = 5
	upstreamRatioSyncMaxIntervalMinutes     = 24 * 60
	upstreamRatioSyncCycleInterval          = time.Minute
	upstreamRatioSyncRequestTimeout         = 15 * time.Second
	upstreamRatioSyncMaxBodyBytes           = 2 * 1024 * 1024
	upstreamRatioSyncMaxPerCycle            = 20
	upstreamRatioSyncConcurrency            = 4
	upstreamRatioSyncLeaderLockKey          = "upstream:ratio:sync:leader"
	upstreamRatioSyncLeaderLockTTL          = 2 * time.Minute
	upstreamRatioSyncMaxModels              = 5000
	upstreamRatioSyncDefaultQuotaPerUnit    = int64(500000)
	upstreamRatioSyncDefaultRetentionDays   = 90
	upstreamRatioSyncMinRetentionDays       = 7
	upstreamRatioSyncNegativeCacheTTL       = 5 * time.Minute

	UpstreamRatioSyncStatusOK           = "ok"
	UpstreamRatioSyncStatusUnsupported  = "unsupported"
	UpstreamRatioSyncStatusFailed       = "failed"
	UpstreamRatioSyncReasonUnauthorized = "unauthorized"
)

var (
	ErrUpstreamRatioSyncUnavailable = infraerrors.ServiceUnavailable(
		"UPSTREAM_RATIO_SYNC_UNAVAILABLE", "upstream ratio sync is unavailable",
	)
	ErrUpstreamRatioSyncAccountInvalid = infraerrors.BadRequest(
		"UPSTREAM_RATIO_SYNC_ACCOUNT_INVALID", "account is not a supported upstream ratio sync account",
	)
)

// UpstreamRatioSyncSettings controls the periodic new-api ratio sync runner.
type UpstreamRatioSyncSettings struct {
	Enabled         bool  `json:"enabled"`
	IntervalMinutes int   `json:"interval_minutes"`
	QuotaPerUnit    int64 `json:"quota_per_unit"`
	RetentionDays   int   `json:"retention_days"`
}

func defaultUpstreamRatioSyncSettings() *UpstreamRatioSyncSettings {
	return &UpstreamRatioSyncSettings{
		Enabled:         true,
		IntervalMinutes: upstreamRatioSyncDefaultIntervalMinutes,
		QuotaPerUnit:    upstreamRatioSyncDefaultQuotaPerUnit,
		RetentionDays:   upstreamRatioSyncDefaultRetentionDays,
	}
}

func normalizeUpstreamRatioSyncSettings(settings *UpstreamRatioSyncSettings) {
	if settings.IntervalMinutes < upstreamRatioSyncMinIntervalMinutes {
		settings.IntervalMinutes = upstreamRatioSyncMinIntervalMinutes
	}
	if settings.IntervalMinutes > upstreamRatioSyncMaxIntervalMinutes {
		settings.IntervalMinutes = upstreamRatioSyncMaxIntervalMinutes
	}
	if settings.QuotaPerUnit <= 0 {
		settings.QuotaPerUnit = upstreamRatioSyncDefaultQuotaPerUnit
	}
	if settings.RetentionDays < upstreamRatioSyncMinRetentionDays {
		settings.RetentionDays = upstreamRatioSyncDefaultRetentionDays
	}
}

// GetUpstreamRatioSyncSettings returns defaults when the setting is absent.
func (s *SettingService) GetUpstreamRatioSyncSettings(ctx context.Context) (*UpstreamRatioSyncSettings, error) {
	defaults := defaultUpstreamRatioSyncSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyUpstreamRatioSyncSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get upstream ratio sync settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaults, nil
	}
	settings := *defaults
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return nil, fmt.Errorf("parse upstream ratio sync settings: %w", err)
	}
	if settings.IntervalMinutes == 0 {
		settings.IntervalMinutes = defaults.IntervalMinutes
	}
	if settings.QuotaPerUnit == 0 {
		settings.QuotaPerUnit = defaults.QuotaPerUnit
	}
	if settings.RetentionDays == 0 {
		settings.RetentionDays = defaults.RetentionDays
	}
	normalizeUpstreamRatioSyncSettings(&settings)
	return &settings, nil
}

// SetUpstreamRatioSyncSettings validates and persists the runner settings.
func (s *SettingService) SetUpstreamRatioSyncSettings(ctx context.Context, settings *UpstreamRatioSyncSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting repository is unavailable")
	}
	if settings == nil {
		return infraerrors.BadRequest("INVALID_UPSTREAM_RATIO_SYNC_SETTINGS", "settings cannot be nil")
	}
	if settings.IntervalMinutes < upstreamRatioSyncMinIntervalMinutes || settings.IntervalMinutes > upstreamRatioSyncMaxIntervalMinutes {
		return infraerrors.BadRequest(
			"INVALID_UPSTREAM_RATIO_SYNC_INTERVAL",
			fmt.Sprintf("interval_minutes must be between %d and %d", upstreamRatioSyncMinIntervalMinutes, upstreamRatioSyncMaxIntervalMinutes),
		)
	}
	if settings.QuotaPerUnit <= 0 {
		return infraerrors.BadRequest(
			"INVALID_UPSTREAM_RATIO_SYNC_QUOTA_PER_UNIT",
			"quota_per_unit must be greater than 0",
		)
	}
	if settings.RetentionDays < upstreamRatioSyncMinRetentionDays {
		return infraerrors.BadRequest(
			"INVALID_UPSTREAM_RATIO_SYNC_RETENTION",
			fmt.Sprintf("retention_days must be at least %d", upstreamRatioSyncMinRetentionDays),
		)
	}
	normalizeUpstreamRatioSyncSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal upstream ratio sync settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyUpstreamRatioSyncSettings, string(data))
}

// UpstreamRatioSyncSnapshot is persisted in accounts.extra. It carries the
// sync bookkeeping only; the per-model ratio table lives in
// upstream_model_ratio_snapshots.
type UpstreamRatioSyncSnapshot struct {
	Status        string     `json:"status"`
	ModelCount    int        `json:"model_count,omitempty"`
	Group         string     `json:"group,omitempty"`
	GroupRatio    *float64   `json:"group_ratio,omitempty"`
	ReceivedAt    *time.Time `json:"received_at,omitempty"`
	FreshUntil    *time.Time `json:"fresh_until,omitempty"`
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	NextSyncAt    time.Time  `json:"next_sync_at"`
	FailureCount  int        `json:"failure_count,omitempty"`
	HTTPStatus    int        `json:"http_status,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// UpstreamRatioSyncResult is returned by manual sync endpoints.
type UpstreamRatioSyncResult struct {
	AccountID int64                      `json:"account_id"`
	Snapshot  *UpstreamRatioSyncSnapshot `json:"snapshot,omitempty"`
	Error     string                     `json:"error,omitempty"`
}

// UpstreamRatioHistoryEntry summarizes one past sync run.
type UpstreamRatioHistoryEntry struct {
	ObservedAt time.Time `json:"observed_at"`
	ModelCount int       `json:"model_count"`
	GroupRatio float64   `json:"group_ratio"`
}

// UpstreamRatioSnapshotRepo persists synced per-model ratio timelines.
type UpstreamRatioSnapshotRepo interface {
	ReplaceUpstreamModelRatioSnapshot(ctx context.Context, accountID int64, observedAt time.Time, rows []UpstreamModelRatio) error
	LatestUpstreamModelRatioRows(ctx context.Context, accountID int64, limit int) ([]UpstreamModelRatio, time.Time, error)
	ListUpstreamRatioSyncHistory(ctx context.Context, accountID int64, limit int) ([]UpstreamRatioHistoryEntry, error)
	DeleteUpstreamModelRatioSnapshotsBefore(ctx context.Context, before time.Time) (int64, error)
}

type upstreamRatioSyncSnapshotWriter interface {
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
}

// UpstreamRatioSyncService mirrors the per-model ratio table of new-api style
// upstreams into a local snapshot timeline and serves request-time cost.
type UpstreamRatioSyncService struct {
	accountRepo        AccountRepository
	accountTestService *AccountTestService
	settingService     *SettingService
	snapshotRepo       UpstreamRatioSnapshotRepo

	parentCtx    context.Context
	parentCancel context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	started      bool
	stopped      bool
	cycleMu      sync.Mutex
	syncGroup    singleflight.Group
	loadGroup    singleflight.Group
	syncSlots    chan struct{}
	now          func() time.Time
	lockCache    LeaderLockCache
	db           *sql.DB
	instanceID   string

	cacheMu sync.RWMutex
	cache   map[int64]*upstreamRatioCacheEntry
	lastGC  time.Time
}

type upstreamRatioCacheEntry struct {
	rows         map[string]UpstreamModelRatio
	observedAt   time.Time
	freshUntil   time.Time
	quotaPerUnit float64
}

func NewUpstreamRatioSyncService(
	accountRepo AccountRepository,
	accountTestService *AccountTestService,
	settingService *SettingService,
	snapshotRepo UpstreamRatioSnapshotRepo,
) *UpstreamRatioSyncService {
	ctx, cancel := context.WithCancel(context.Background())
	return &UpstreamRatioSyncService{
		accountRepo:        accountRepo,
		accountTestService: accountTestService,
		settingService:     settingService,
		snapshotRepo:       snapshotRepo,
		parentCtx:          ctx,
		parentCancel:       cancel,
		syncSlots:          make(chan struct{}, upstreamRatioSyncConcurrency),
		now:                time.Now,
		instanceID:         uuid.NewString(),
		cache:              map[int64]*upstreamRatioCacheEntry{},
	}
}

func (s *UpstreamRatioSyncService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// ProvideUpstreamRatioSyncService starts the process-wide periodic runner.
func ProvideUpstreamRatioSyncService(
	accountRepo AccountRepository,
	accountTestService *AccountTestService,
	settingService *SettingService,
	snapshotRepo UpstreamRatioSnapshotRepo,
	lockCache LeaderLockCache,
	db *sql.DB,
) *UpstreamRatioSyncService {
	svc := NewUpstreamRatioSyncService(accountRepo, accountTestService, settingService, snapshotRepo)
	svc.SetLeaderLock(lockCache, db)
	svc.Start()
	return svc
}

func (s *UpstreamRatioSyncService) Start() {
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

func (s *UpstreamRatioSyncService) Stop() {
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

func (s *UpstreamRatioSyncService) runLoop() {
	defer s.wg.Done()
	_ = s.RunDue(s.parentCtx)
	ticker := time.NewTicker(upstreamRatioSyncCycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.parentCtx.Done():
			return
		case <-ticker.C:
			if err := s.RunDue(s.parentCtx); err != nil {
				logger.LegacyPrintf("service.upstream_ratio_sync", "run_due_failed: err=%v", err)
			}
		}
	}
}

// RunDue executes at most one bounded batch of due accounts.
func (s *UpstreamRatioSyncService) RunDue(ctx context.Context) error {
	if s == nil || s.accountRepo == nil {
		return nil
	}
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	settings, err := s.getSettings(ctx)
	if err != nil {
		return err
	}
	s.cleanupExpiredSnapshots(ctx, settings)
	if !settings.Enabled {
		return nil
	}
	runRelease, acquired, lockErr := s.tryAcquireLeaderLock(ctx, upstreamRatioSyncLeaderLockKey)
	if lockErr != nil {
		return fmt.Errorf("acquire upstream ratio sync leader lock: %w", lockErr)
	}
	if !acquired {
		return nil
	}
	defer runRelease()

	lockNow := time.Now()
	cadenceRelease, acquired, lockErr := s.tryAcquireLeaderLock(ctx, upstreamRatioSyncLeaderLockKeyAt(lockNow))
	if lockErr != nil {
		return fmt.Errorf("acquire upstream ratio sync cadence lock: %w", lockErr)
	}
	if !acquired {
		return nil
	}
	defer releaseUpstreamBillingProbeLeaderLock(cadenceRelease, lockNow.Truncate(upstreamRatioSyncCycleInterval).Add(upstreamRatioSyncCycleInterval))

	now := s.currentTime()
	accounts, err := s.accountRepo.FindByExtraField(ctx, UpstreamRatioSyncEnabledExtraKey, true)
	if err != nil {
		return fmt.Errorf("list enabled upstream ratio sync accounts: %w", err)
	}
	due := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if !isUpstreamRatioSyncAccount(&account) || !account.IsActive() {
			continue
		}
		snapshot := decodeUpstreamRatioSyncSnapshot(account.Extra)
		if snapshot != nil && !snapshot.NextSyncAt.IsZero() && now.Before(snapshot.NextSyncAt) {
			continue
		}
		due = append(due, account)
	}
	sort.SliceStable(due, func(i, j int) bool {
		left := decodeUpstreamRatioSyncSnapshot(due[i].Extra)
		right := decodeUpstreamRatioSyncSnapshot(due[j].Extra)
		leftUnset := left == nil || left.NextSyncAt.IsZero()
		rightUnset := right == nil || right.NextSyncAt.IsZero()
		if leftUnset && rightUnset {
			return due[i].ID < due[j].ID
		}
		if leftUnset {
			return true
		}
		if rightUnset {
			return false
		}
		return left.NextSyncAt.Before(right.NextSyncAt)
	})
	if len(due) > upstreamRatioSyncMaxPerCycle {
		due = due[:upstreamRatioSyncMaxPerCycle]
	}

	var group errgroup.Group
	for i := range due {
		accountID := due[i].ID
		group.Go(func() error {
			if _, syncErr := s.syncScheduledAccount(ctx, accountID, settings.IntervalMinutes); syncErr != nil {
				logger.LegacyPrintf("service.upstream_ratio_sync", "sync_due_failed: account_id=%d err=%v", accountID, syncErr)
			}
			return nil
		})
	}
	return group.Wait()
}

func (s *UpstreamRatioSyncService) cleanupExpiredSnapshots(ctx context.Context, settings *UpstreamRatioSyncSettings) {
	if s == nil || s.snapshotRepo == nil {
		return
	}
	now := s.currentTime()
	if !s.lastGC.IsZero() && now.Sub(s.lastGC) < 24*time.Hour {
		return
	}
	s.lastGC = now
	before := now.AddDate(0, 0, -settings.RetentionDays)
	deleted, err := s.snapshotRepo.DeleteUpstreamModelRatioSnapshotsBefore(ctx, before)
	if err != nil {
		logger.LegacyPrintf("service.upstream_ratio_sync", "cleanup_failed: err=%v", err)
		return
	}
	if deleted > 0 {
		logger.LegacyPrintf("service.upstream_ratio_sync", "cleanup_deleted=%d before=%s", deleted, before.Format(time.RFC3339))
	}
}

func (s *UpstreamRatioSyncService) getSettings(ctx context.Context) (*UpstreamRatioSyncSettings, error) {
	if s.settingService == nil {
		return defaultUpstreamRatioSyncSettings(), nil
	}
	return s.settingService.GetUpstreamRatioSyncSettings(ctx)
}

func (s *UpstreamRatioSyncService) GetSettings(ctx context.Context) (*UpstreamRatioSyncSettings, error) {
	return s.getSettings(ctx)
}

func (s *UpstreamRatioSyncService) UpdateSettings(ctx context.Context, settings *UpstreamRatioSyncSettings) error {
	if s == nil || s.settingService == nil {
		return ErrUpstreamRatioSyncUnavailable
	}
	return s.settingService.SetUpstreamRatioSyncSettings(ctx, settings)
}

// SyncAccount performs one manual sync; manual calls ignore both switches.
func (s *UpstreamRatioSyncService) SyncAccount(ctx context.Context, accountID int64) (*UpstreamRatioSyncSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrUpstreamRatioSyncUnavailable
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.syncAccount(ctx, accountID, settings.IntervalMinutes)
}

func (s *UpstreamRatioSyncService) syncAccount(ctx context.Context, accountID int64, intervalMinutes int) (*UpstreamRatioSyncSnapshot, error) {
	return s.syncAccountWithMode(ctx, accountID, intervalMinutes, false)
}

func (s *UpstreamRatioSyncService) syncScheduledAccount(ctx context.Context, accountID int64, intervalMinutes int) (*UpstreamRatioSyncSnapshot, error) {
	return s.syncAccountWithMode(ctx, accountID, intervalMinutes, true)
}

func (s *UpstreamRatioSyncService) syncAccountWithMode(ctx context.Context, accountID int64, intervalMinutes int, requireEnabled bool) (*UpstreamRatioSyncSnapshot, error) {
	key := strconv.FormatInt(accountID, 10)
	value, err, _ := s.syncGroup.Do(key, func() (any, error) {
		select {
		case s.syncSlots <- struct{}{}:
			defer func() { <-s.syncSlots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		account, loadErr := s.accountRepo.GetByID(ctx, accountID)
		if loadErr != nil {
			return nil, loadErr
		}
		if !isUpstreamRatioSyncAccount(account) {
			return nil, ErrUpstreamRatioSyncAccountInvalid
		}
		if requireEnabled {
			if !account.IsActive() || !upstreamRatioSyncEnabled(account) {
				return nil, nil
			}
			if snapshot := decodeUpstreamRatioSyncSnapshot(account.Extra); snapshot != nil &&
				!snapshot.NextSyncAt.IsZero() && s.currentTime().Before(snapshot.NextSyncAt) {
				return nil, nil
			}
		}
		return s.syncLoadedAccount(ctx, account, intervalMinutes)
	})
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	snapshot, ok := value.(*UpstreamRatioSyncSnapshot)
	if !ok {
		return nil, fmt.Errorf("invalid upstream ratio sync result")
	}
	return snapshot, nil
}

// SyncAccounts performs a bounded manual batch with the runner's concurrency limit.
func (s *UpstreamRatioSyncService) SyncAccounts(ctx context.Context, accountIDs []int64) []UpstreamRatioSyncResult {
	if len(accountIDs) > upstreamRatioSyncMaxPerCycle {
		accountIDs = accountIDs[:upstreamRatioSyncMaxPerCycle]
	}
	results := make([]UpstreamRatioSyncResult, len(accountIDs))
	if s == nil || s.accountRepo == nil {
		for i, accountID := range accountIDs {
			results[i] = UpstreamRatioSyncResult{AccountID: accountID, Error: ErrUpstreamRatioSyncUnavailable.Error()}
		}
		return results
	}
	settings, settingsErr := s.getSettings(ctx)
	if settingsErr != nil {
		for i, accountID := range accountIDs {
			results[i] = UpstreamRatioSyncResult{AccountID: accountID, Error: safeUpstreamRatioSyncError(settingsErr)}
		}
		return results
	}
	var group errgroup.Group
	for i, accountID := range accountIDs {
		i, accountID := i, accountID
		results[i].AccountID = accountID
		group.Go(func() error {
			snapshot, err := s.syncAccount(ctx, accountID, settings.IntervalMinutes)
			if err != nil {
				results[i].Error = safeUpstreamRatioSyncError(err)
				return nil
			}
			results[i].Snapshot = snapshot
			return nil
		})
	}
	_ = group.Wait()
	return results
}

// SetAccountEnabled flips the per-account switch in accounts.extra.
func (s *UpstreamRatioSyncService) SetAccountEnabled(ctx context.Context, accountID int64, enabled bool) error {
	if s == nil || s.accountRepo == nil {
		return ErrUpstreamRatioSyncUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if !isUpstreamRatioSyncAccount(account) {
		return ErrUpstreamRatioSyncAccountInvalid
	}
	return s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		UpstreamRatioSyncEnabledExtraKey: enabled,
	})
}

// SetAccountGroup stores the new-api group name used to resolve group_ratio.
func (s *UpstreamRatioSyncService) SetAccountGroup(ctx context.Context, accountID int64, group string) error {
	if s == nil || s.accountRepo == nil {
		return ErrUpstreamRatioSyncUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if !isUpstreamRatioSyncAccount(account) {
		return ErrUpstreamRatioSyncAccountInvalid
	}
	group = strings.TrimSpace(group)
	if len(group) > 200 {
		return infraerrors.BadRequest("INVALID_UPSTREAM_RATIO_SYNC_GROUP", "group name must be at most 200 characters")
	}
	if group == "" {
		group = "default"
	}
	return s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		UpstreamRatioSyncGroupExtraKey: group,
	})
}

// GetAccountResult returns the current snapshot plus the latest ratio table.
func (s *UpstreamRatioSyncService) GetAccountResult(ctx context.Context, accountID int64, modelSearch string, limit int) (*UpstreamRatioSyncSnapshot, []UpstreamModelRatio, []UpstreamRatioHistoryEntry, error) {
	if s == nil || s.accountRepo == nil {
		return nil, nil, nil, ErrUpstreamRatioSyncUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isUpstreamRatioSyncAccount(account) {
		return nil, nil, nil, ErrUpstreamRatioSyncAccountInvalid
	}
	snapshot := decodeUpstreamRatioSyncSnapshot(account.Extra)
	if s.snapshotRepo == nil {
		return snapshot, nil, nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, _, err := s.snapshotRepo.LatestUpstreamModelRatioRows(ctx, accountID, upstreamRatioSyncMaxModels)
	if err != nil {
		return snapshot, nil, nil, err
	}
	if search := strings.ToLower(strings.TrimSpace(modelSearch)); search != "" {
		filtered := make([]UpstreamModelRatio, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.ModelName), search) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	history, err := s.snapshotRepo.ListUpstreamRatioSyncHistory(ctx, accountID, 20)
	if err != nil {
		return snapshot, rows, nil, nil
	}
	return snapshot, rows, history, nil
}

func upstreamRatioSyncLeaderLockKeyAt(now time.Time) string {
	return fmt.Sprintf("%s:%d", upstreamRatioSyncLeaderLockKey, now.Unix()/int64(upstreamRatioSyncCycleInterval/time.Second))
}

func (s *UpstreamRatioSyncService) tryAcquireLeaderLock(ctx context.Context, key string) (func(), bool, error) {
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.lockCache != nil {
		acquired, err := s.lockCache.TryAcquireLeaderLock(lockCtx, key, s.instanceID, upstreamRatioSyncLeaderLockTTL)
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

func (s *UpstreamRatioSyncService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func safeUpstreamRatioSyncError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrUpstreamRatioSyncAccountInvalid) {
		return ErrUpstreamRatioSyncAccountInvalid.Error()
	}
	if errors.Is(err, ErrUpstreamRatioSyncUnavailable) {
		return ErrUpstreamRatioSyncUnavailable.Error()
	}
	return "sync_failed"
}

func decodeUpstreamRatioSyncSnapshot(extra map[string]any) *UpstreamRatioSyncSnapshot {
	if extra == nil {
		return nil
	}
	value, ok := extra[UpstreamRatioSyncExtraKey]
	if !ok {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot UpstreamRatioSyncSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Status == "" {
		return nil
	}
	if snapshot.Status != UpstreamRatioSyncStatusOK &&
		snapshot.Status != UpstreamRatioSyncStatusUnsupported &&
		snapshot.Status != UpstreamRatioSyncStatusFailed {
		return nil
	}
	return &snapshot
}

// isUpstreamRatioSyncAccount accepts any API-key account with an explicit
// custom base_url — new-api style upstreams exist for every platform we proxy.
func isUpstreamRatioSyncAccount(account *Account) bool {
	return account != nil && account.Type == AccountTypeAPIKey &&
		strings.TrimSpace(account.GetCredential("base_url")) != ""
}

func upstreamRatioSyncEnabled(account *Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	enabled, ok := account.Extra[UpstreamRatioSyncEnabledExtraKey].(bool)
	return ok && enabled
}

func upstreamRatioSyncGroup(account *Account) string {
	if account == nil {
		return "default"
	}
	group := strings.TrimSpace(account.GetExtraString(UpstreamRatioSyncGroupExtraKey))
	if group == "" {
		return "default"
	}
	return group
}
