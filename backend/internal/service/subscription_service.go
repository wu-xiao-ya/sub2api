package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/dgraph-io/ristretto"
	"golang.org/x/sync/singleflight"
)

// MaxExpiresAt is the maximum allowed expiration date (year 2099)
// This prevents time.Time JSON serialization errors (RFC 3339 requires year <= 9999)
var MaxExpiresAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

// MaxValidityDays is the maximum allowed validity days for subscriptions (100 years)
const MaxValidityDays = 36500

var (
	ErrSubscriptionNotFound        = infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")
	ErrSubscriptionExpired         = infraerrors.Forbidden("SUBSCRIPTION_EXPIRED", "subscription has expired")
	ErrSubscriptionSuspended       = infraerrors.Forbidden("SUBSCRIPTION_SUSPENDED", "subscription is suspended")
	ErrSubscriptionAlreadyExists   = infraerrors.Conflict("SUBSCRIPTION_ALREADY_EXISTS", "subscription already exists for this user and group")
	ErrSubscriptionAssignConflict  = infraerrors.Conflict("SUBSCRIPTION_ASSIGN_CONFLICT", "subscription exists but request conflicts with existing assignment semantics")
	ErrSubscriptionNotRevoked      = infraerrors.Conflict("SUBSCRIPTION_NOT_REVOKED", "subscription is not revoked")
	ErrSubscriptionRestoreConflict = infraerrors.Conflict("SUBSCRIPTION_RESTORE_CONFLICT", "subscription already exists for this user and group")
	ErrGroupNotSubscriptionType    = infraerrors.BadRequest("GROUP_NOT_SUBSCRIPTION_TYPE", "group is not a subscription type")
	ErrInvalidInput                = infraerrors.BadRequest("INVALID_INPUT", "at least one of resetDaily, resetWeekly, or resetMonthly must be true")
	ErrDailyLimitExceeded          = infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily usage limit exceeded")
	ErrWeeklyLimitExceeded         = infraerrors.TooManyRequests("WEEKLY_LIMIT_EXCEEDED", "weekly usage limit exceeded")
	ErrMonthlyLimitExceeded        = infraerrors.TooManyRequests("MONTHLY_LIMIT_EXCEEDED", "monthly usage limit exceeded")
	ErrSubscriptionNilInput        = infraerrors.BadRequest("SUBSCRIPTION_NIL_INPUT", "subscription input cannot be nil")
	ErrAdjustWouldExpire           = infraerrors.BadRequest("ADJUST_WOULD_EXPIRE", "adjustment would result in expired subscription (remaining days must be > 0)")
	ErrNativeSubscriptionRetired   = infraerrors.Forbidden("NATIVE_SUBSCRIPTION_RETIRED", "native subscriptions are retired; use subscription purchases")
)

// SubscriptionService 订阅服务
type SubscriptionService struct {
	groupRepo           GroupRepository
	userSubRepo         UserSubscriptionRepository
	billingCacheService *BillingCacheService
	entClient           *dbent.Client
	sqlDB               *sql.DB

	// L1 缓存：加速中间件热路径的订阅查询
	subCacheL1     *ristretto.Cache
	subCacheGroup  singleflight.Group
	subCacheTTL    time.Duration
	subCacheJitter int // 抖动百分比

	maintenanceQueue *SubscriptionMaintenanceQueue
}

// NewSubscriptionService 创建订阅服务
func NewSubscriptionService(groupRepo GroupRepository, userSubRepo UserSubscriptionRepository, billingCacheService *BillingCacheService, entClient *dbent.Client, cfg *config.Config, sqlDBs ...*sql.DB) *SubscriptionService {
	var sqlDB *sql.DB
	if len(sqlDBs) > 0 {
		sqlDB = sqlDBs[0]
	}
	svc := &SubscriptionService{
		groupRepo:           groupRepo,
		userSubRepo:         userSubRepo,
		billingCacheService: billingCacheService,
		entClient:           entClient,
		sqlDB:               sqlDB,
	}
	svc.initSubCache(cfg)
	svc.initMaintenanceQueue(cfg)
	svc.StartSubCacheInvalidationSubscriber(context.Background())
	return svc
}

func (s *SubscriptionService) initMaintenanceQueue(cfg *config.Config) {
	if cfg == nil {
		return
	}
	mc := cfg.SubscriptionMaintenance
	if mc.WorkerCount <= 0 || mc.QueueSize <= 0 {
		return
	}
	s.maintenanceQueue = NewSubscriptionMaintenanceQueue(mc.WorkerCount, mc.QueueSize)
}

// Stop stops the maintenance worker pool.
func (s *SubscriptionService) Stop() {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		s.maintenanceQueue.Stop()
	}
}

// initSubCache 初始化订阅 L1 缓存
func (s *SubscriptionService) initSubCache(cfg *config.Config) {
	if cfg == nil {
		return
	}
	sc := cfg.SubscriptionCache
	if sc.L1Size <= 0 || sc.L1TTLSeconds <= 0 {
		return
	}
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(sc.L1Size) * 10,
		MaxCost:     int64(sc.L1Size),
		BufferItems: 64,
	})
	if err != nil {
		log.Printf("Warning: failed to init subscription L1 cache: %v", err)
		return
	}
	s.subCacheL1 = cache
	s.subCacheTTL = time.Duration(sc.L1TTLSeconds) * time.Second
	s.subCacheJitter = sc.JitterPercent
}

// subCacheKey 生成订阅缓存 key（热路径，避免 fmt.Sprintf 开销）
func subCacheKey(userID, groupID int64) string {
	return "sub:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(groupID, 10)
}

// jitteredTTL 为 TTL 添加抖动，避免集中过期
func (s *SubscriptionService) jitteredTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 || s.subCacheJitter <= 0 {
		return ttl
	}
	pct := s.subCacheJitter
	if pct > 100 {
		pct = 100
	}
	delta := float64(pct) / 100
	factor := 1 - delta + rand.Float64()*(2*delta)
	if factor <= 0 {
		return ttl
	}
	return time.Duration(float64(ttl) * factor)
}

// InvalidateSubCache 失效指定用户+分组的订阅 L1 缓存
func (s *SubscriptionService) InvalidateSubCache(userID, groupID int64) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(subCacheKey(userID, groupID))
}

// InvalidateSubCacheSync 失效订阅 L1 缓存并等待 Ristretto 删除操作生效。
func (s *SubscriptionService) InvalidateSubCacheSync(userID, groupID int64) {
	s.invalidateSubCacheKeySync(subCacheKey(userID, groupID))
}

func (s *SubscriptionService) invalidateSubCacheKeySync(key string) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(key)
	s.subCacheL1.Wait()
}

// StartSubCacheInvalidationSubscriber 启动跨实例订阅 L1 缓存失效订阅。
func (s *SubscriptionService) StartSubCacheInvalidationSubscriber(ctx context.Context) {
	if s.billingCacheService == nil || s.subCacheL1 == nil {
		return
	}
	if err := s.billingCacheService.SubscribeSubscriptionCacheInvalidation(ctx, func(cacheKey string) {
		s.invalidateSubCacheKeySync(cacheKey)
	}); err != nil {
		log.Printf("Warning: failed to start subscription cache invalidation subscriber: %v", err)
	}
}

func (s *SubscriptionService) invalidateSubscriptionCaches(userID, groupID int64) error {
	s.InvalidateSubCacheSync(userID, groupID)
	if s.billingCacheService == nil {
		return nil
	}

	cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID); err != nil {
		return fmt.Errorf("invalidate billing subscription cache: %w", err)
	}
	if err := s.billingCacheService.PublishSubscriptionCacheInvalidation(cacheCtx, subCacheKey(userID, groupID)); err != nil {
		return fmt.Errorf("publish subscription cache invalidation: %w", err)
	}
	return nil
}

// AssignSubscriptionInput 分配订阅输入
type AssignSubscriptionInput struct {
	UserID       int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

// DefaultSubscriptionAssigner is retained only as a compile-time compatibility
// boundary for authentication flows. The current SubscriptionService
// implementation fails closed and does not write user_subscriptions.
type DefaultSubscriptionAssigner interface {
	AssignOrExtendSubscription(context.Context, *AssignSubscriptionInput) (*UserSubscription, bool, error)
}

// Native subscription operations are retired. The method signatures remain as
// compatibility shims for old callers and generated wiring, but no request may
// create or mutate rows in the frozen user_subscriptions table.
func (s *SubscriptionService) AssignSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) AssignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	return nil, false, ErrNativeSubscriptionRetired
}

type BulkAssignSubscriptionInput struct {
	UserIDs      []int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

type BulkAssignResult struct {
	SuccessCount  int
	CreatedCount  int
	ReusedCount   int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Statuses      map[int64]string
}

func (s *SubscriptionService) BulkAssignSubscription(ctx context.Context, input *BulkAssignSubscriptionInput) (*BulkAssignResult, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) RevokeSubscription(ctx context.Context, subscriptionID int64) error {
	return ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) RestoreSubscription(ctx context.Context, subscriptionID int64) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ExtendSubscription(ctx context.Context, subscriptionID int64, days int) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ListUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ListGroupSubscriptions(ctx context.Context, groupID int64, page, pageSize int) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) List(ctx context.Context, page, pageSize int, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) CheckAndActivateWindow(ctx context.Context, sub *UserSubscription) error {
	return ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) AdminResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) CheckAndResetWindows(ctx context.Context, sub *UserSubscription) error {
	return ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription) (*UserSubscription, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) CheckUsageLimits(ctx context.Context, sub *UserSubscription, group *Group, additionalCost float64) error {
	return ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ValidateAndCheckLimits(sub *UserSubscription, group *Group) (needsMaintenance bool, err error) {
	return false, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) DoWindowMaintenance(sub *UserSubscription) {}

func (s *SubscriptionService) RecordUsage(ctx context.Context, subscriptionID int64, costUSD float64) error {
	return ErrNativeSubscriptionRetired
}

type SubscriptionProgress struct {
	ID            int64                `json:"id"`
	GroupName     string               `json:"group_name"`
	ExpiresAt     time.Time            `json:"expires_at"`
	ExpiresInDays int                  `json:"expires_in_days"`
	Daily         *UsageWindowProgress `json:"daily,omitempty"`
	Weekly        *UsageWindowProgress `json:"weekly,omitempty"`
	Monthly       *UsageWindowProgress `json:"monthly,omitempty"`
}

type UsageWindowProgress struct {
	LimitUSD        float64   `json:"limit_usd"`
	UsedUSD         float64   `json:"used_usd"`
	RemainingUSD    float64   `json:"remaining_usd"`
	Percentage      float64   `json:"percentage"`
	WindowStart     time.Time `json:"window_start"`
	ResetsAt        time.Time `json:"resets_at"`
	ResetsInSeconds int64     `json:"resets_in_seconds"`
}

func (s *SubscriptionService) GetSubscriptionProgress(ctx context.Context, subscriptionID int64) (*SubscriptionProgress, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) GetUserSubscriptionsWithProgress(ctx context.Context, userID int64) ([]SubscriptionProgress, error) {
	return nil, ErrNativeSubscriptionRetired
}

func (s *SubscriptionService) ValidateSubscription(ctx context.Context, sub *UserSubscription) error {
	return ErrNativeSubscriptionRetired
}

// These pure helpers remain available to compatibility tests and DTO code; they
// never perform database I/O or mutate the retired subscription table.
func normalizeExpiredWindows(subs []UserSubscription) {
	for i := range subs {
		sub := &subs[i]
		if sub.NeedsDailyReset() {
			sub.DailyWindowStart = nil
			sub.DailyUsageUSD = 0
		}
		if sub.NeedsWeeklyReset() {
			sub.WeeklyWindowStart = nil
			sub.WeeklyUsageUSD = 0
		}
		if sub.NeedsMonthlyReset() {
			sub.MonthlyWindowStart = nil
			sub.MonthlyUsageUSD = 0
		}
	}
}

func normalizeSubscriptionStatus(subs []UserSubscription) {
	now := time.Now()
	for i := range subs {
		if subs[i].Status == SubscriptionStatusActive && !subs[i].ExpiresAt.After(now) {
			subs[i].Status = SubscriptionStatusExpired
		}
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func appendSubscriptionNotes(existingNotes, newNotes string) string {
	if newNotes == "" {
		return existingNotes
	}
	if existingNotes == "" {
		return newNotes
	}
	return existingNotes + "\n" + newNotes
}

func normalizeAssignValidityDays(days int) int {
	if days <= 0 {
		return 30
	}
	if days > MaxValidityDays {
		return MaxValidityDays
	}
	return days
}
