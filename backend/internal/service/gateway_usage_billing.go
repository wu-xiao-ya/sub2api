package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func (s *GatewayService) getUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if s == nil {
		return groupDefaultMultiplier
	}
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(
			s.userGroupRateRepo,
			s.userGroupRateCache,
			resolveUserGroupRateCacheTTL(s.cfg),
			&s.userGroupRateSF,
			"service.gateway",
		)
	}
	return resolver.Resolve(ctx, userID, groupID, groupDefaultMultiplier)
}

// ResolveUserGroupRateMultiplier resolves the same cached multiplier used by usage billing.
func (s *GatewayService) ResolveUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	return s.getUserGroupRateMultiplier(ctx, userID, groupID, groupDefaultMultiplier)
}

// RecordUsageInput ???????????
// ?? worker ?????????????? ParsedRequest/RequestBodyRef ?????????
type RecordUsageInput struct {
	Result             *ForwardResult
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       *UserSubscription  // ???????
	InboundEndpoint    string             // ?????????????
	UpstreamEndpoint   string             // ???????????????
	UserAgent          string             // ??? User-Agent
	IPAddress          string             // ?????? IP ??
	RequestPayloadHash string             // ???????????? request_id ????????????
	ForceCacheBilling  bool               // ???????? input_tokens ?? cache_read ????????????
	APIKeyService      APIKeyQuotaUpdater // ???????API Key??
	QuotaPlatform      string             // user?platform ???????handler ??? ctx ?? QuotaPlatform() ??????????? worker ? background ctx ????? ForcePlatform?

	ChannelUsageFields // ???????? handler ? Forward ????
}

// APIKeyQuotaUpdater defines the interface for updating API Key quota and rate limit usage
type APIKeyQuotaUpdater interface {
	UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error
	UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error
}

type apiKeyAuthCacheInvalidator interface {
	InvalidateAuthCacheByKey(ctx context.Context, key string)
}

type usageLogBestEffortWriter interface {
	CreateBestEffort(ctx context.Context, log *UsageLog) error
}

// postUsageBillingParams ?????????
type postUsageBillingParams struct {
	Cost                  *CostBreakdown
	User                  *User
	APIKey                *APIKey
	Account               *Account
	Subscription          *UserSubscription
	RequestPayloadHash    string
	IsSubscriptionBill    bool
	AccountRateMultiplier float64
	APIKeyService         APIKeyQuotaUpdater
	Platform              string // ?? APIKey ?? Group ?????
}

// PlatformFromAPIKey ? APIKey ??? Group ?? platform ???
// apiKey ? nil ? Group ??????????????? short-circuit quota ????
// ??? handler ????
func PlatformFromAPIKey(apiKey *APIKey) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

// QuotaPlatform ?? user?platform ????????????
// ???????? /antigravity???? ctx ?? ForcePlatform ????????
// APIKey ?? Group ????
//
// ??????? ForcePlatform ??? context ???? handler ? c.Request.Context()??
// ????? worker ?? background ctx ??? ForcePlatform???????? handler
// ?????? RecordUsageInput.QuotaPlatform ??????????? worker ctx ??????
func QuotaPlatform(ctx context.Context, apiKey *APIKey) string {
	if fp, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && fp != "" {
		return fp
	}
	return PlatformFromAPIKey(apiKey)
}

func (p *postUsageBillingParams) shouldDeductAPIKeyQuota() bool {
	return p.Cost.ActualCost > 0 && p.APIKey.Quota > 0 && p.APIKeyService != nil
}

func (p *postUsageBillingParams) shouldUpdateRateLimits() bool {
	return p.Cost.ActualCost > 0 && p.APIKey.HasRateLimits() && p.APIKeyService != nil
}

func (p *postUsageBillingParams) shouldUpdateAccountQuota() bool {
	return p.Cost.TotalCost > 0 && p.Account.IsAPIKeyOrBedrock() && p.Account.HasAnyQuotaLimit()
}

// postUsageBilling is the legacy fallback billing path used when the unified
// billing repo is unavailable (nil). Production uses applyUsageBilling ? repo.Apply
// for atomic billing. This path only runs in tests or degraded mode.
type subscriptionPurchaseUsageIncrementer interface {
	IncrementPurchaseUsage(ctx context.Context, purchaseID int64, costUSD float64) error
}

func postUsageBilling(ctx context.Context, p *postUsageBillingParams, deps *billingDeps) {
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	cost := p.Cost

	if p.IsSubscriptionBill {
		// Degraded billing must use the same explicit purchase identity as the
		// atomic repository. Native user_subscriptions is read-only after the
		// retirement migration and must never receive request-time usage writes.
		if cost.ActualCost > 0 && p.Subscription != nil {
			purchaseID := p.Subscription.SubscriptionPurchaseID
			incrementer, ok := deps.userSubRepo.(subscriptionPurchaseUsageIncrementer)
			if purchaseID == nil || *purchaseID <= 0 {
				slog.Error("legacy subscription billing is disabled", "user_id", p.Subscription.UserID, "group_id", p.Subscription.GroupID)
			} else if !ok {
				slog.Error("purchase usage fallback is unavailable", "subscription_purchase_id", *purchaseID)
			} else if err := incrementer.IncrementPurchaseUsage(billingCtx, *purchaseID, cost.ActualCost); err != nil {
				slog.Error("increment purchase usage failed", "subscription_purchase_id", *purchaseID, "error", err)
			}
		}
	} else {
		if cost.ActualCost > 0 {
			if err := deps.userRepo.DeductBalance(billingCtx, p.User.ID, cost.ActualCost); err != nil {
				slog.Error("deduct balance failed", "user_id", p.User.ID, "error", err)
			} else if deps.billingCacheService != nil {
				if err := deps.billingCacheService.InvalidateUserBalance(billingCtx, p.User.ID); err != nil {
					slog.Warn("invalidate balance cache after legacy deduction failed", "user_id", p.User.ID, "error", err)
				}
			}
		}
	}

	if p.shouldDeductAPIKeyQuota() {
		if err := p.APIKeyService.UpdateQuotaUsed(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key quota failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateRateLimits() {
		if err := p.APIKeyService.UpdateRateLimitUsage(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key rate limit usage failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateAccountQuota() {
		accountCost := cost.TotalCost * p.AccountRateMultiplier
		if err := deps.accountRepo.IncrementQuotaUsed(billingCtx, p.Account.ID, accountCost); err != nil {
			slog.Error("increment account quota used failed", "account_id", p.Account.ID, "cost", accountCost, "error", err)
		}
	}

	// Platform quota ???legacy ???????? standard??????????????????? limit ????
	//   - HasUserPlatformQuotaLimit ??:????????? limit ????
	//   - ?? Redis ???:enforcement ? Redis?legacy ??????????? preflight ?????
	//   - flusher_enabled=false????:???????? DB
	//   - flusher_enabled=true:???? DB?? flusher ??????markDirty ? IncrementUserPlatformQuotaUsage ?????
	//   - ???? ALERT log + counter?????????
	if !p.IsSubscriptionBill && p.Platform != "" && cost.ActualCost > 0 && p.User != nil && deps.userPlatformQuotaRepo != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(billingCtx, p.User.ID, p.Platform) {
			deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, cost.ActualCost)
			if deps.cfg == nil || !deps.cfg.Database.UserPlatformQuotaFlusherEnabled {
				// ????:flusher ???????????? DB
				if err := deps.userPlatformQuotaRepo.IncrementUsageWithReset(billingCtx, p.User.ID, p.Platform, cost.ActualCost, time.Now().UTC()); err != nil {
					userPlatformQuotaDBIncrLegacyErrorTotal.Add(1)
					logger.LegacyPrintf("service.gateway", "ALERT: legacy incr user platform quota DB failed user=%d platform=%s cost=%f: %v", p.User.ID, p.Platform, cost.ActualCost, err)
				}
			}
			// flusher_enabled=true:??? DB?flusher ?????
		}
	}

	// NOTE: finalizePostUsageBilling is NOT called here to avoid double-queuing
	// cache updates. The legacy path does DB writes directly; the finalize path
	// does cache queue + notifications. Notifications are dispatched separately
	// by the caller after recording the usage log.
}

func resolveUsageBillingRequestID(ctx context.Context, upstreamRequestID string) string {
	// Forced durable money-event IDs must win over client/local context IDs so
	// standalone web_search / async video cannot collapse under a reused client id.
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		if isForcedUsageBillingRequestID(requestID) {
			return requestID
		}
	}
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		return requestID
	}
	return "generated:" + generateRequestID()
}

func isForcedUsageBillingRequestID(requestID string) bool {
	id := strings.TrimSpace(requestID)
	return strings.HasPrefix(id, "web_search:") ||
		strings.HasPrefix(id, "grok-video:") ||
		strings.HasPrefix(id, "grok_audio:") ||
		strings.HasPrefix(id, "grok_realtime:")
}

// StableGrokAudioBillingRequestID is the durable usage_logs / dedup key for one
// voice HTTP call (TTS/STT). Prefer an upstream request id when present.
func StableGrokAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "grok_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "grok_audio:" + upstreamRequestID
}

// StableGrokRealtimeBillingRequestID is the durable usage_logs / dedup key for
// one realtime WebSocket session.
func StableGrokRealtimeBillingRequestID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if strings.HasPrefix(sessionID, "grok_realtime:") {
		return sessionID
	}
	if sessionID == "" {
		sessionID = generateRequestID()
	}
	return "grok_realtime:" + sessionID
}

func resolveUsageBillingPayloadFingerprint(ctx context.Context, requestPayloadHash string) string {
	if payloadHash := strings.TrimSpace(requestPayloadHash); payloadHash != "" {
		return payloadHash
	}
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	return ""
}

func buildUsageBillingCommand(requestID string, usageLog *UsageLog, p *postUsageBillingParams) *UsageBillingCommand {
	if p == nil || p.Cost == nil || p.APIKey == nil || p.User == nil || p.Account == nil {
		return nil
	}

	cmd := &UsageBillingCommand{
		RequestID:          requestID,
		APIKeyID:           p.APIKey.ID,
		UserID:             p.User.ID,
		AccountID:          p.Account.ID,
		AccountType:        p.Account.Type,
		RequestPayloadHash: strings.TrimSpace(p.RequestPayloadHash),
	}
	if usageLog != nil {
		cmd.Model = usageLog.Model
		cmd.BillingType = usageLog.BillingType
		cmd.InputTokens = usageLog.InputTokens
		cmd.OutputTokens = usageLog.OutputTokens
		cmd.CacheCreationTokens = usageLog.CacheCreationTokens
		cmd.CacheReadTokens = usageLog.CacheReadTokens
		cmd.ImageCount = usageLog.ImageCount
		if usageLog.ServiceTier != nil {
			cmd.ServiceTier = *usageLog.ServiceTier
		}
		if usageLog.ReasoningEffort != nil {
			cmd.ReasoningEffort = *usageLog.ReasoningEffort
		}
		if usageLog.SubscriptionPurchaseID != nil {
			cmd.SubscriptionPurchaseID = usageLog.SubscriptionPurchaseID
		}
	}

	// Record subscription / balance cost using ActualCost so the group (and any
	// user-specific) rate multiplier consumes subscription quota at the expected
	// speed. TotalCost remains the raw (pre-multiplier) value; downstream guards
	// on "> 0" still correctly skip free subscriptions (RateMultiplier == 0).
	if p.IsSubscriptionBill && p.Subscription != nil && p.Cost.TotalCost > 0 {
		if p.Subscription.SubscriptionPurchaseID != nil {
			cmd.SubscriptionPurchaseID = p.Subscription.SubscriptionPurchaseID
			cmd.SubscriptionID = nil
		}
		cmd.SubscriptionCost = p.Cost.ActualCost
	} else if p.Cost.ActualCost > 0 {
		cmd.BalanceCost = p.Cost.ActualCost
	}

	if p.shouldDeductAPIKeyQuota() {
		cmd.APIKeyQuotaCost = p.Cost.ActualCost
	}
	if p.shouldUpdateRateLimits() {
		cmd.APIKeyRateLimitCost = p.Cost.ActualCost
	}
	if p.shouldUpdateAccountQuota() {
		cmd.AccountQuotaCost = p.Cost.TotalCost * p.AccountRateMultiplier
	}

	// ???????????????????????????????????
	// ?????????????????????
	if p.Account.OwnerUserID != nil &&
		p.Account.ContributionStatus == ContributionStatusApproved &&
		p.APIKey.Group != nil &&
		p.APIKey.Group.ContributorRewardMultiplier > 0 &&
		p.Cost.TotalCost > 0 &&
		p.Cost.ActualCost > 0 {
		reward := p.Cost.TotalCost * p.APIKey.Group.ContributorRewardMultiplier
		if reward > p.Cost.ActualCost {
			reward = p.Cost.ActualCost
		}
		if reward > 0 {
			cmd.ContributorOwnerUserID = *p.Account.OwnerUserID
			cmd.ContributorRewardAccountID = p.Account.ID
			if p.APIKey.GroupID != nil {
				cmd.ContributorRewardGroupID = *p.APIKey.GroupID
			} else {
				cmd.ContributorRewardGroupID = p.APIKey.Group.ID
			}
			cmd.ContributorRewardMultiplier = p.APIKey.Group.ContributorRewardMultiplier
			cmd.ContributorRewardTotalCost = p.Cost.TotalCost
			cmd.ContributorRewardActualCost = p.Cost.ActualCost
			cmd.ContributorRewardAmount = reward
		}
	}

	cmd.Normalize()
	return cmd
}

func applyUsageBilling(ctx context.Context, requestID string, usageLog *UsageLog, p *postUsageBillingParams, deps *billingDeps, repo UsageBillingRepository) (bool, error) {
	if p == nil || deps == nil {
		return false, nil
	}

	cmd := buildUsageBillingCommand(requestID, usageLog, p)
	if cmd == nil || cmd.RequestID == "" || repo == nil {
		postUsageBilling(ctx, p, deps)
		return true, nil
	}

	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	result, err := repo.Apply(billingCtx, cmd)
	if err != nil {
		return false, err
	}

	if result == nil || !result.Applied {
		deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)
		return false, nil
	}

	if result.APIKeyQuotaExhausted {
		if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheInvalidator); ok && p.APIKey != nil && p.APIKey.Key != "" {
			invalidator.InvalidateAuthCacheByKey(billingCtx, p.APIKey.Key)
		}
	}
	if result.ConsumptionConcurrencyChanged {
		if invalidator, ok := p.APIKeyService.(APIKeyAuthCacheInvalidator); ok && p.User != nil {
			invalidator.InvalidateAuthCacheByUserID(billingCtx, p.User.ID)
		}
	}

	finalizePostUsageBilling(billingCtx, p, deps, result)
	return true, nil
}

func finalizePostUsageBilling(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || p.Cost == nil || deps == nil {
		return
	}

	if p.IsSubscriptionBill {
		if p.Cost.ActualCost > 0 && p.User != nil && p.APIKey != nil && p.APIKey.GroupID != nil {
			deps.billingCacheService.QueueUpdateSubscriptionUsage(p.User.ID, *p.APIKey.GroupID, p.Cost.ActualCost)
		}
	} else if p.Cost.ActualCost > 0 && p.User != nil {
		syncBalanceCacheAfterDeduction(ctx, p, deps, result)
	}

	if result != nil && result.ContributorRewardApplied && result.ContributorRewardOwnerUserID > 0 && deps.billingCacheService != nil {
		if err := deps.billingCacheService.InvalidateUserBalance(ctx, result.ContributorRewardOwnerUserID); err != nil {
			slog.Warn("invalidate contributor balance cache failed", "user_id", result.ContributorRewardOwnerUserID, "error", err)
		}
	}

	if p.Cost.ActualCost > 0 && p.APIKey != nil && p.APIKey.HasRateLimits() {
		deps.billingCacheService.QueueUpdateAPIKeyRateLimitUsage(p.APIKey.ID, p.Cost.ActualCost)
	}

	deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)

	// Platform quota ????? standard??????????????????? limit ????
	// Redis ??? + DB ??????flag=false ???? flusher ????flag=true?:
	//   - HasUserPlatformQuotaLimit ??:? limit ?????,?????? + ?? Redis ??
	//   - Redis ??:???? preflight ?????? usage,? TOCTOU ????
	//     ????? in-flight ???????????????????????? worker ???
	//   - DB ??(flusher_enabled=false):??? goroutine ?? detached context,??? ALERT log ?? oncall ??
	//   - flusher_enabled=true:??? DB,? flusher ??????markDirty ?? IncrementUserPlatformQuotaUsage ?????
	if !p.IsSubscriptionBill && p.Platform != "" && p.Cost.ActualCost > 0 && p.User != nil && deps.userPlatformQuotaRepo != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(ctx, p.User.ID, p.Platform) {
			deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, p.Cost.ActualCost)
			if deps.cfg == nil || !deps.cfg.Database.UserPlatformQuotaFlusherEnabled {
				// ????:flusher ???????????? DB
				dbCtx, dbCancel := detachUpstreamContext(ctx)
				userID, platform, cost := p.User.ID, p.Platform, p.Cost.ActualCost
				go func() {
					defer func() {
						if r := recover(); r != nil {
							logger.LegacyPrintf("service.gateway", "ALERT: panic in user platform quota incr goroutine user=%d platform=%s: %v", userID, platform, r)
						}
					}()
					defer dbCancel()
					if err := deps.userPlatformQuotaRepo.IncrementUsageWithReset(dbCtx, userID, platform, cost, time.Now().UTC()); err != nil {
						// ?????:??? GatewayUserPlatformQuotaIncrStats(),? ops ????????
						userPlatformQuotaDBIncrErrorTotal.Add(1)
						// ALERT ??:DB ???????? Redis cache ????? cost ????,
						// ??????????????,oncall ????????????
						logger.LegacyPrintf("service.gateway", "ALERT: incr user platform quota DB failed user=%d platform=%s cost=%f: %v", userID, platform, cost, err)
					}
				}()
			}
			// flusher_enabled=true:??? DB,flusher ?????
		}
	}

	// Notification checks run async ? all parameters are already captured,
	// no dependency on the request context or upstream connection.
	go notifyBalanceLow(p, deps, result)
	go notifyAccountQuota(p, deps, result)
}

func syncBalanceCacheAfterDeduction(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || p.Cost == nil || p.User == nil || deps == nil || deps.billingCacheService == nil {
		return
	}
	if result != nil && result.NewBalance != nil && deps.billingCacheService.balanceBelowEligibilityThreshold(*result.NewBalance) {
		if err := deps.billingCacheService.InvalidateUserBalance(ctx, p.User.ID); err != nil {
			slog.Warn("invalidate balance cache after exhausted deduction failed",
				"user_id", p.User.ID,
				"new_balance", *result.NewBalance,
				"balance_overdrafted", result.BalanceOverdrafted,
				"error", err,
			)
		}
		return
	}
	deps.billingCacheService.QueueDeductBalance(p.User.ID, p.Cost.ActualCost)
}

// notifyBalanceLow sends balance low notification after deduction.
// When result.NewBalance is available (from DB transaction RETURNING), it is used directly
// to reconstruct oldBalance, avoiding stale Redis reads and concurrent-deduction races.
func notifyBalanceLow(p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in notifyBalanceLow", "recover", r)
		}
	}()
	if p.IsSubscriptionBill || p.Cost.ActualCost <= 0 || p.User == nil || deps.balanceNotifyService == nil {
		slog.Debug("notifyBalanceLow: skipped",
			"is_subscription", p.IsSubscriptionBill,
			"actual_cost", p.Cost.ActualCost,
			"user_nil", p.User == nil,
			"service_nil", deps.balanceNotifyService == nil,
		)
		return
	}

	oldBalance := resolveOldBalance(p, result)
	slog.Debug("notifyBalanceLow: calling CheckBalanceAfterDeduction",
		"user_id", p.User.ID,
		"old_balance", oldBalance,
		"cost", p.Cost.ActualCost,
		"notify_enabled", p.User.BalanceNotifyEnabled,
		"threshold", p.User.BalanceNotifyThreshold,
		"result_has_new_balance", result != nil && result.NewBalance != nil,
	)
	deps.balanceNotifyService.CheckBalanceAfterDeduction(context.Background(), p.User, oldBalance, p.Cost.ActualCost)
}

// resolveOldBalance returns the pre-deduction balance.
// Prefers the DB transaction result (newBalance + cost) over snapshot.
func resolveOldBalance(p *postUsageBillingParams, result *UsageBillingApplyResult) float64 {
	if result != nil && result.NewBalance != nil {
		return *result.NewBalance + p.Cost.ActualCost
	}
	// Legacy fallback: snapshot balance from request context
	return p.User.Balance
}

// notifyAccountQuota sends account quota threshold notification after increment.
// When result.QuotaState is available (from DB transaction RETURNING), it is passed directly
// to avoid a separate DB read that may see stale or concurrently-modified data.
func notifyAccountQuota(p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in notifyAccountQuota", "recover", r)
		}
	}()
	if p.Cost.TotalCost <= 0 || p.Account == nil || !p.Account.IsAPIKeyOrBedrock() || deps.balanceNotifyService == nil {
		slog.Debug("notifyAccountQuota: skipped",
			"total_cost", p.Cost.TotalCost,
			"account_nil", p.Account == nil,
			"is_apikey_or_bedrock", p.Account != nil && p.Account.IsAPIKeyOrBedrock(),
			"service_nil", deps.balanceNotifyService == nil,
		)
		return
	}
	accountCost := p.Cost.TotalCost * p.AccountRateMultiplier
	var quotaState *AccountQuotaState
	if result != nil {
		quotaState = result.QuotaState
	}
	slog.Debug("notifyAccountQuota: calling CheckAccountQuotaAfterIncrement",
		"account_id", p.Account.ID,
		"account_cost", accountCost,
		"has_quota_state", quotaState != nil,
	)
	deps.balanceNotifyService.CheckAccountQuotaAfterIncrement(context.Background(), p.Account, accountCost, quotaState)
}

func detachedBillingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, postUsageBillingTimeout)
}

func detachStreamUpstreamContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	if !stream {
		return ctx, func() {}
	}
	return context.WithoutCancel(ctx), func() {}
}

func detachUpstreamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	return context.WithoutCancel(ctx), func() {}
}

// billingDeps ???????????? gateway service ???
type billingDeps struct {
	accountRepo           AccountRepository
	userRepo              UserRepository
	userSubRepo           UserSubscriptionRepository
	billingCacheService   *BillingCacheService
	deferredService       *DeferredService
	balanceNotifyService  *BalanceNotifyService
	userPlatformQuotaRepo UserPlatformQuotaRepository
	cfg                   *config.Config
}

func (s *GatewayService) billingDeps() *billingDeps {
	return &billingDeps{
		accountRepo:           s.accountRepo,
		userRepo:              s.userRepo,
		userSubRepo:           s.userSubRepo,
		billingCacheService:   s.billingCacheService,
		deferredService:       s.deferredService,
		balanceNotifyService:  s.balanceNotifyService,
		userPlatformQuotaRepo: s.userPlatformQuotaRepo,
		cfg:                   s.cfg,
	}
}

func writeUsageLogBestEffort(ctx context.Context, repo UsageLogRepository, usageLog *UsageLog, logKey string) {
	if repo == nil || usageLog == nil {
		return
	}
	usageCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	if writer, ok := repo.(usageLogBestEffortWriter); ok {
		if err := writer.CreateBestEffort(usageCtx, usageLog); err != nil {
			logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
			// ????????????????dropped?????????????????
			// ??????????? usage_log???????issue #3656??
			// ????? usage_logs ? ON CONFLICT (request_id, api_key_id) DO NOTHING ???
			fallbackCtx := usageCtx
			if usageCtx.Err() != nil {
				// usageCtx ????best-effort ???????????? detached ????????????
				var fallbackCancel context.CancelFunc
				fallbackCtx, fallbackCancel = detachedBillingContext(context.Background())
				defer fallbackCancel()
			}
			if _, syncErr := repo.Create(fallbackCtx, usageLog); syncErr != nil {
				logger.LegacyPrintf(logKey, "Create usage log sync fallback failed: %v", syncErr)
			}
		}
		return
	}

	if _, err := repo.Create(usageCtx, usageLog); err != nil {
		logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
	}
}

// recordUsageOpts ????????????????????????
type recordUsageOpts struct {
	// ???????? Gemini ?????
	LongContextThreshold  int
	LongContextMultiplier float64
}

// RecordUsage ?????????????????
func (s *GatewayService) RecordUsage(ctx context.Context, input *RecordUsageInput) error {
	if IsChannelMonitorRequest(ctx) {
		return nil
	}
	return s.recordUsageCore(ctx, &recordUsageCoreInput{
		Result:             input.Result,
		APIKey:             input.APIKey,
		User:               input.User,
		Account:            input.Account,
		Subscription:       input.Subscription,
		InboundEndpoint:    input.InboundEndpoint,
		UpstreamEndpoint:   input.UpstreamEndpoint,
		UserAgent:          input.UserAgent,
		IPAddress:          input.IPAddress,
		RequestPayloadHash: input.RequestPayloadHash,
		ForceCacheBilling:  input.ForceCacheBilling,
		APIKeyService:      input.APIKeyService,
		QuotaPlatform:      input.QuotaPlatform,
		ChannelUsageFields: input.ChannelUsageFields,
	}, &recordUsageOpts{})
}

// RecordUsageLongContextInput ??????????????????????
type RecordUsageLongContextInput struct {
	Result                *ForwardResult
	APIKey                *APIKey
	User                  *User
	Account               *Account
	Subscription          *UserSubscription  // ???????
	InboundEndpoint       string             // ?????????????
	UpstreamEndpoint      string             // ???????????????
	UserAgent             string             // ??? User-Agent
	IPAddress             string             // ?????? IP ??
	RequestPayloadHash    string             // ???????????? request_id ????????????
	LongContextThreshold  int                // ???????? 200000?
	LongContextMultiplier float64            // ??????????? 2.0?
	ForceCacheBilling     bool               // ???????? input_tokens ?? cache_read ????????????
	APIKeyService         APIKeyQuotaUpdater // API Key ????????
	QuotaPlatform         string             // user?platform ???????handler ??? ctx ?? QuotaPlatform() ??????????? worker ? background ctx ????? ForcePlatform?

	ChannelUsageFields // ???????? handler ? Forward ????
}

// RecordUsageWithLongContext ?????????????????????? Gemini?
func (s *GatewayService) RecordUsageWithLongContext(ctx context.Context, input *RecordUsageLongContextInput) error {
	return s.recordUsageCore(ctx, &recordUsageCoreInput{
		Result:             input.Result,
		APIKey:             input.APIKey,
		User:               input.User,
		Account:            input.Account,
		Subscription:       input.Subscription,
		InboundEndpoint:    input.InboundEndpoint,
		UpstreamEndpoint:   input.UpstreamEndpoint,
		UserAgent:          input.UserAgent,
		IPAddress:          input.IPAddress,
		RequestPayloadHash: input.RequestPayloadHash,
		ForceCacheBilling:  input.ForceCacheBilling,
		APIKeyService:      input.APIKeyService,
		QuotaPlatform:      input.QuotaPlatform,
		ChannelUsageFields: input.ChannelUsageFields,
	}, &recordUsageOpts{
		LongContextThreshold:  input.LongContextThreshold,
		LongContextMultiplier: input.LongContextMultiplier,
	})
}

// recordUsageCoreInput ? recordUsageCore ????????????????????
type recordUsageCoreInput struct {
	Result             *ForwardResult
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       *UserSubscription
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	RequestPayloadHash string
	ForceCacheBilling  bool
	APIKeyService      APIKeyQuotaUpdater
	QuotaPlatform      string
	ChannelUsageFields
}

// recordUsageCore ? RecordUsage ? RecordUsageWithLongContext ??????
// LongContextThreshold > 0 ? Token ????? CalculateCostWithLongContext?
func (s *GatewayService) recordUsageCore(ctx context.Context, input *recordUsageCoreInput, opts *recordUsageOpts) error {
	result := input.Result
	apiKey := input.APIKey
	user := input.User
	account := input.Account
	subscription := effectiveSubscriptionForBilling(ctx, input.Subscription)
	ApplyForwardImageBillingResolution(result)

	// ???????? input_tokens ?? cache_read_input_tokens
	// ????????????????
	if input.ForceCacheBilling && result.Usage.InputTokens > 0 {
		logger.LegacyPrintf("service.gateway", "force_cache_billing: %d input_tokens ? cache_read_input_tokens (account=%d)",
			result.Usage.InputTokens, account.ID)
		result.Usage.CacheReadInputTokens += result.Usage.InputTokens
		result.Usage.InputTokens = 0
	}

	// Cache TTL Override: ????? token ??????????
	// ?????????? 1h ??????????? usage ???? 5m?
	cacheTTLOverridden := false
	if overrideTarget, ok := s.resolveCacheTTLUsageOverrideTarget(ctx, account); ok {
		applyCacheTTLOverride(&result.Usage, overrideTarget)
		cacheTTLOverridden = (result.Usage.CacheCreation5mTokens + result.Usage.CacheCreation1hTokens) > 0
	}

	// ??????????????? > ???? > ?????
	multiplier := 1.0
	if s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if apiKey.GroupID != nil && apiKey.Group != nil {
		groupDefault := apiKey.Group.RateMultiplier
		multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, groupDefault)
	}
	// Resolve the user's normal rate first, then high-peak/media rules, and
	// finally the activity. The promotion therefore lowers the real charge
	// without ever bypassing a more favorable user-specific rate.
	now := timezone.Now()
	textBaseMultiplier, imageBaseMultiplier := computePeakAwareMultipliers(apiKey, multiplier, now)
	groupID := int64(0)
	if apiKey.GroupID != nil {
		groupID = *apiKey.GroupID
	}
	multiplier, textPromotion := applyCurrentGroupPromotion(ctx, groupID, textBaseMultiplier, now)
	imageMultiplier, imagePromotion := applyCurrentGroupPromotion(ctx, groupID, imageBaseMultiplier, now)

	// ??????
	billingModel := forwardResultBillingModel(result.Model, result.UpstreamModel)
	if input.BillingModelSource == BillingModelSourceChannelMapped && input.ChannelMappedModel != "" {
		billingModel = input.ChannelMappedModel
	}
	if input.BillingModelSource == BillingModelSourceRequested && input.OriginalModel != "" {
		billingModel = input.OriginalModel
	}

	// ?? RequestedModel????????????
	requestedModel := result.Model
	if input.OriginalModel != "" {
		requestedModel = input.OriginalModel
	}

	// ????
	cost := s.calculateRecordUsageCost(ctx, result, apiKey, billingModel, multiplier, imageMultiplier, opts)

	// ??????????? vs ????
	isSubscriptionBilling := subscription != nil
	billingType := BillingTypeBalance
	if isSubscriptionBilling {
		billingType = BillingTypeSubscription
	}

	// ??????
	accountRateMultiplier := account.BillingRateMultiplier()
	usageLog := s.buildRecordUsageLog(ctx, input, result, apiKey, user, account, subscription,
		requestedModel, multiplier, imageMultiplier, textPromotion, imagePromotion, accountRateMultiplier, billingType, cacheTTLOverridden, cost, opts)

	// ???????????????????????????
	if apiKey.GroupID != nil {
		applyAccountStatsCost(ctx, usageLog, s.channelService, s.billingService,
			account.ID, *apiKey.GroupID, result.UpstreamModel, result.Model,
			// Anthropic's input_tokens excludes cache_read and cache_creation (billed separately);
			// OpenAI gateway uses actualInputTokens which also excludes cache_read for the same reason.
			UsageTokens{
				InputTokens:         result.Usage.InputTokens,
				OutputTokens:        result.Usage.OutputTokens,
				CacheCreationTokens: result.Usage.CacheCreationInputTokens,
				CacheReadTokens:     result.Usage.CacheReadInputTokens,
				ImageOutputTokens:   result.Usage.ImageOutputTokens,
			},
			cost.TotalCost,
		)
	}

	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
		logger.LegacyPrintf("service.gateway", "[SIMPLE MODE] Usage recorded (not billed): user=%d, tokens=%d", usageLog.UserID, usageLog.TotalTokens())
		s.deferredService.ScheduleLastUsedUpdate(account.ID)
		return nil
	}

	// ????? handler ??? ctx ?? QuotaPlatform() ????? input ???
	// ????? worker ?? background ctx ?????? ctx ? ForcePlatform?
	// ????????????????????????????
	quotaPlatform := input.QuotaPlatform
	if quotaPlatform == "" {
		quotaPlatform = PlatformFromAPIKey(apiKey)
	}
	requestID := usageLog.RequestID
	_, billingErr := applyUsageBilling(ctx, requestID, usageLog, &postUsageBillingParams{
		Cost:                  cost,
		User:                  user,
		APIKey:                apiKey,
		Account:               account,
		Subscription:          subscription,
		RequestPayloadHash:    resolveUsageBillingPayloadFingerprint(ctx, input.RequestPayloadHash),
		IsSubscriptionBill:    isSubscriptionBilling,
		AccountRateMultiplier: accountRateMultiplier,
		APIKeyService:         input.APIKeyService,
		Platform:              quotaPlatform,
	}, s.billingDeps(), s.usageBillingRepo)

	if billingErr != nil {
		usageLog.ActualCost = 0
		writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
		return billingErr
	}
	writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")

	return nil
}

// calculateRecordUsageCost ??????????????
func (s *GatewayService) calculateRecordUsageCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	imageMultiplier float64,
	opts *recordUsageOpts,
) *CostBreakdown {
	// ?????????? token ???? token ??????????
	if result.ImageCount > 0 {
		if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil && resolved.Mode == BillingModeToken {
			return s.calculateTokenCost(ctx, result, apiKey, billingModel, multiplier, opts)
		}
		return s.calculateImageCost(ctx, result, apiKey, billingModel, imageMultiplier)
	}

	// Voice audio (TTS / STT / realtime) when present on the forward result.
	if result.AudioUsage != nil {
		cfg := groupAudioPriceConfigFromAPIKey(apiKey)
		return s.billingService.CalculateAudioCost(result.AudioUsage.Mode, result.AudioUsage.DurationOrUnits, cfg, multiplier)
	}

	// Token ???SearchCount ??? surcharge???? token??
	tokenCost := s.calculateTokenCost(ctx, result, apiKey, billingModel, multiplier, opts)
	if result.SearchCount > 0 {
		price := groupSearchPricePer1kFromAPIKey(apiKey)
		if price == nil || *price <= 0 {
			logger.LegacyPrintf("service.gateway", "[Billing] search_price_per_1k unset; search calls free group_model=%s count=%d", billingModel, result.SearchCount)
		}
		searchCost := s.billingService.CalculateSearchCost(result.SearchCount, price, multiplier)
		if searchCost != nil && (searchCost.TotalCost > 0 || searchCost.ActualCost > 0) {
			if tokenCost == nil {
				return searchCost
			}
			tokenCost.TotalCost += searchCost.TotalCost
			tokenCost.ActualCost += searchCost.ActualCost
		}
	}
	return tokenCost
}

// resolveChannelPricing ?????????????????
// ??? nil ? ResolvedPricing ????????nil ??????????
func (s *GatewayService) resolveChannelPricing(ctx context.Context, billingModel string, apiKey *APIKey) *ResolvedPricing {
	if s.resolver == nil || apiKey.Group == nil {
		return nil
	}
	gid := apiKey.Group.ID
	resolved := s.resolver.Resolve(ctx, PricingInput{Model: billingModel, GroupID: &gid})
	if resolved.Source == PricingSourceChannel {
		return resolved
	}
	return nil
}

// calculateImageCost ??????????????????????????
func (s *GatewayService) calculateImageCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
) *CostBreakdown {
	sizeTier := NormalizeImageBillingTierOrDefault(result.ImageSize)
	groupConfig := imagePriceConfigFromAPIKey(apiKey)
	if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
		return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier)
	}
	if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil {
		tokens := UsageTokens{
			InputTokens:       result.Usage.InputTokens,
			OutputTokens:      result.Usage.OutputTokens,
			ImageOutputTokens: result.Usage.ImageOutputTokens,
		}
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			Tokens:         tokens,
			RequestCount:   result.ImageCount,
			SizeTier:       sizeTier,
			RateMultiplier: multiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
		if err != nil {
			logger.LegacyPrintf("service.gateway", "Calculate image token cost failed: %v", err)
			return &CostBreakdown{ActualCost: 0}
		}
		return cost
	}

	return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier)
}

// calculateTokenCost ?? Token ????? opts ?????/????/???????
func (s *GatewayService) calculateTokenCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	opts *recordUsageOpts,
) *CostBreakdown {
	tokens := UsageTokens{
		InputTokens:           result.Usage.InputTokens,
		OutputTokens:          result.Usage.OutputTokens,
		CacheCreationTokens:   result.Usage.CacheCreationInputTokens,
		CacheReadTokens:       result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:     result.Usage.ImageOutputTokens,
	}

	var cost *CostBreakdown
	var err error

	// ???????? ? CalculateCostUnified
	if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil {
		gid := apiKey.Group.ID
		cost, err = s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			Tokens:         tokens,
			RequestCount:   1,
			RateMultiplier: multiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
	} else if opts.LongContextThreshold > 0 {
		// ?????????? Gemini 200K ???
		cost, err = s.billingService.CalculateCostWithLongContext(billingModel, tokens, multiplier, opts.LongContextThreshold, opts.LongContextMultiplier)
	} else {
		cost, err = s.billingService.CalculateCost(billingModel, tokens, multiplier)
	}
	if err != nil {
		logger.LegacyPrintf("service.gateway", "Calculate cost failed: %v", err)
		return &CostBreakdown{ActualCost: 0}
	}
	return cost
}

// buildRecordUsageLog ??????????????
func (s *GatewayService) buildRecordUsageLog(
	ctx context.Context,
	input *recordUsageCoreInput,
	result *ForwardResult,
	apiKey *APIKey,
	user *User,
	account *Account,
	subscription *UserSubscription,
	requestedModel string,
	multiplier float64,
	imageMultiplier float64,
	textPromotion *AppliedGroupPromotion,
	imagePromotion *AppliedGroupPromotion,
	accountRateMultiplier float64,
	billingType int8,
	cacheTTLOverridden bool,
	cost *CostBreakdown,
	opts *recordUsageOpts,
) *UsageLog {
	durationMs := int(result.Duration.Milliseconds())
	requestID := resolveUsageBillingRequestID(ctx, result.RequestID)
	usageLog := &UsageLog{
		UserID:                 user.ID,
		APIKeyID:               apiKey.ID,
		AccountID:              account.ID,
		RequestID:              requestID,
		Model:                  result.Model,
		RequestedModel:         requestedModel,
		UpstreamModel:          optionalNonEqualStringPtr(result.UpstreamModel, requestedModel),
		ReasoningEffort:        result.ReasoningEffort,
		InboundEndpoint:        optionalTrimmedStringPtr(input.InboundEndpoint),
		UpstreamEndpoint:       optionalTrimmedStringPtr(input.UpstreamEndpoint),
		UsageSource:            usageSourceFromContext(ctx),
		InputTokens:            result.Usage.InputTokens,
		OutputTokens:           result.Usage.OutputTokens,
		CacheCreationTokens:    result.Usage.CacheCreationInputTokens,
		CacheReadTokens:        result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens:  result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens:  result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:      result.Usage.ImageOutputTokens,
		RateMultiplier:         multiplier,
		AccountRateMultiplier:  &accountRateMultiplier,
		BillingType:            billingType,
		BillingMode:            resolveBillingMode(result, cost),
		Stream:                 result.Stream,
		DurationMs:             &durationMs,
		FirstTokenMs:           result.FirstTokenMs,
		LatencyBreakdown:       result.LatencyBreakdown.Clone(),
		ImageCount:             result.ImageCount,
		ImageSize:              optionalTrimmedStringPtr(result.ImageSize),
		ImageInputSize:         optionalTrimmedStringPtr(result.ImageInputSize),
		ImageOutputSize:        optionalTrimmedStringPtr(result.ImageOutputSize),
		ImageSizeSource:        optionalTrimmedStringPtr(result.ImageSizeSource),
		ImageSizeBreakdown:     result.ImageSizeBreakdown,
		CacheTTLOverridden:     cacheTTLOverridden,
		ChannelID:              optionalInt64Ptr(input.ChannelID),
		ModelMappingChain:      optionalTrimmedStringPtr(input.ModelMappingChain),
		UserAgent:              optionalTrimmedStringPtr(input.UserAgent),
		IPAddress:              optionalTrimmedStringPtr(input.IPAddress),
		GroupID:                apiKey.GroupID,
		SubscriptionPurchaseID: optionalSubscriptionPurchaseID(subscription),
		CreatedAt:              time.Now(),
	}
	if result.ImageCount > 0 && (cost == nil || cost.BillingMode != string(BillingModeToken)) {
		usageLog.RateMultiplier = imageMultiplier
		applyUsageLogPromotionSnapshot(usageLog, imagePromotion)
	} else {
		applyUsageLogPromotionSnapshot(usageLog, textPromotion)
	}
	if cost != nil {
		usageLog.InputCost = cost.InputCost
		usageLog.OutputCost = cost.OutputCost
		usageLog.ImageOutputCost = cost.ImageOutputCost
		usageLog.CacheCreationCost = cost.CacheCreationCost
		usageLog.CacheReadCost = cost.CacheReadCost
		usageLog.TotalCost = cost.TotalCost
		usageLog.ActualCost = cost.ActualCost
		usageLog.LongContextBillingApplied = cost.LongContextBillingApplied
	}

	return usageLog
}

// resolveBillingMode ??????????????????
func resolveBillingMode(result *ForwardResult, cost *CostBreakdown) *string {
	var mode string
	switch {
	case cost != nil && cost.BillingMode != "":
		mode = cost.BillingMode
	case result.ImageCount > 0:
		mode = string(BillingModeImage)
	default:
		mode = string(BillingModeToken)
	}
	return &mode
}

func optionalSubscriptionPurchaseID(subscription *UserSubscription) *int64 {
	if subscription != nil {
		return subscription.SubscriptionPurchaseID
	}
	return nil
}

// optionalSubscriptionID remains as a source-compatibility helper for older
// tests and adapters. Native subscription IDs are intentionally never emitted
// into billing commands after the retirement migration.
func optionalSubscriptionID(subscription *UserSubscription) *int64 {
	return nil
}
