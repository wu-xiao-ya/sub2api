package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const maxAPIKeyAuthorizationHeaderBytes = service.MaxAPIKeyCredentialBytes + 128

// NewAPIKeyAuthMiddleware ?? API Key ?????
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(apiKeyService, subscriptionService, cfg))
}

// apiKeyAuthWithSubscription API Key?????????????
//
// ??????????
//   - ???Authentication???? Key ?????????IP ?? ?? ????
//   - ?????Billing Enforcement????/??/??/???? ?? skipBilling ?????
//
// /v1/usage?/v1/sub2api/billing ????????????????????????
// usage ????/????? Key ???????billing ?????? Key ??????
// ?????????????? Key ?????????
func apiKeyAuthWithSubscription(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ?? 1. ?? API Key ??????????????????????????????????????????
		if rejectInvalidAuthAbuse(c, apiKeyService) {
			AbortWithError(c, http.StatusTooManyRequests, "INVALID_AUTH_RATE_LIMITED", "Too many invalid authentication attempts; retry later")
			return
		}

		if apiKeyHeadersTooLarge(c) {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectQueryAPIKeyDeprecated)
			AbortWithError(c, 400, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}

		// ???Authorization header???API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// ??Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// ??Authorization header???????x-api-key header???
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}
		if len(apiKeyString) > service.MaxAPIKeyCredentialBytes {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			AbortWithError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}

		// ??x-api-key header???????x-goog-api-key header????Gemini CLI???
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// ????header???API key
		if apiKeyString == "" {
			recordInvalidAuthFailure(c, apiKeyService)
			if hasAPIKeyCredentialInput(c) {
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			} else {
				MarkIngressRejected(c, IngressRejectAPIKeyRequired)
			}
			AbortWithError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		// ?? 2. ?? Key ?? ?????????????????????????????????????????

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				recordInvalidAuthFailure(c, apiKeyService)
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
				AbortWithError(c, 401, "INVALID_API_KEY", "Invalid API key")
				return
			}
			if errors.Is(err, service.ErrAPIKeyAuthOverloaded) {
				MarkIngressRejected(c, IngressRejectAPIKeyAuthOverloaded)
				AbortWithError(c, http.StatusServiceUnavailable, "API_KEY_AUTH_OVERLOADED", "API key authentication is temporarily unavailable")
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to validate API key")
			return
		}

		// apiKey ????? User/Group???????????/Key ??/????/
		// IP ?????????? Ops ????????? user/group/platform?
		SetOpsFallbackAPIKey(c, apiKey)

		// ?? 3. ?????????? ?????????????????????????????????

		// disabled / ???? ? ??????expired ? quota_exhausted ???????
		if !apiKey.IsActive() &&
			apiKey.Status != service.StatusAPIKeyExpired &&
			apiKey.Status != service.StatusAPIKeyQuotaExhausted {
			MarkIngressRejected(c, IngressRejectAPIKeyDisabled)
			AbortWithError(c, 401, "API_KEY_DISABLED", "API key is disabled")
			return
		}

		// ?? IP ??????/????
		// ??????????????????? IP ????
		if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
			clientIP := ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
			if !allowed {
				if clientIP == "" {
					clientIP = "unknown"
				}
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
				MarkIngressRejected(c, IngressRejectIPRestricted)
				AbortWithError(c, 403, "ACCESS_DENIED", fmt.Sprintf("Access denied. Your IP is %s", clientIP))
				return
			}
		}

		// ???????
		if apiKey.User == nil {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User associated with API key not found")
			return
		}

		// ??????
		if !apiKey.User.IsActive() {
			MarkIngressRejected(c, IngressRejectUserInactive)
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}
		if abortIfAPIKeyGroupUnavailable(c, apiKey) {
			return
		}
		if abortIfAPIKeyGroupNotAllowed(c, apiKey) {
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, apiKey.User.ID)
		c.Request = c.Request.WithContext(ctx)
		billingInfoRequest := c.Request.URL.Path == "/v1/sub2api/billing"
		// Async image task polling only reads data that already belongs to the
		// authenticated key and must remain available after the completed
		// generation consumes the key's remaining balance.
		skipBilling := c.Request.URL.Path == "/v1/usage" || billingInfoRequest || isAsyncImageTaskRead(c.Request.Method, c.Request.URL.Path) ||
			service.IsChannelMonitorRequest(c.Request.Context())

		// ?? 4. SimpleMode ? early return ?????????????????????????????

		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			if !billingInfoRequest {
				_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			}
			c.Next()
			return
		}

		// ?? 5. ??????????? subscription_purchases? ??????????

		var subscription *service.UserSubscription
		var sharedPurchase *service.SharedSubscriptionEntitlement
		var sharedConcurrencyPlan *service.SubscriptionConcurrencyPlan
		isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()

		// Purchase snapshots are the only runtime entitlement source. A snapshot
		// authorizes every group recorded in subscription_purchase_groups, so this
		// lookup must not be narrowed to one platform (previously OpenAI-only).
		planLoaded := false
		if apiKey.Group != nil && subscriptionService != nil && !billingInfoRequest {
			if plan, planErr := sharedSubscriptionConcurrencyPlan(
				c.Request.Context(),
				subscriptionService,
				apiKey.User.ID,
				apiKey.Group.ID,
				apiKey.User.Concurrency,
			); planErr == nil {
				planLoaded = true
				sharedConcurrencyPlan = plan
				for i := range plan.Entitlements {
					entitlement := &plan.Entitlements[i]
					if entitlement.SharedSubscription == nil {
						continue
					}
					entitlement.Subscription = entitlement.SharedSubscription.AsLegacySubscription(apiKey.Group)
					if sharedPurchase == nil {
						sharedPurchase = entitlement.SharedSubscription
						subscription = entitlement.Subscription
						c.Set(string(ContextKeySharedSubscription), sharedPurchase)
					}
				}
				if sharedPurchase == nil && !plan.AllowBalanceTopup && !plan.AllowBalancePriority {
					AbortWithError(c, http.StatusTooManyRequests, "USAGE_LIMIT_EXCEEDED", "All active subscription quotas for this group are exhausted")
					return
				}
			}
		}

		// Subscription-type groups have no legacy user_subscriptions fallback:
		// without a purchase snapshot the request carries no entitlement.
		// skipBilling endpoints (/v1/usage, billing introspection, async image
		// task reads) stay reachable so a key can still read its own data.
		if isSubscriptionType && !planLoaded && !skipBilling {
			AbortWithError(c, 403, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group")
			return
		}

		// ?? 6. ?????skipBilling ?????? ????????????????????

		if !skipBilling {
			// Key ????
			switch apiKey.Status {
			case service.StatusAPIKeyQuotaExhausted:
				abortWithAPIKeyQuotaError(c)
				return
			case service.StatusAPIKeyExpired:
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key ???")
				return
			}

			// ?????/?????????? active???????????
			if apiKey.IsExpired() {
				AbortWithError(c, 403, "API_KEY_EXPIRED", "API key ???")
				return
			}
			if apiKey.IsQuotaExhausted() {
				abortWithAPIKeyQuotaError(c)
				return
			}

			// Subscription mode: validate the purchase snapshot directly.
			// user_subscriptions is never read here; rolling quota windows are
			// already normalised by the purchase reader, so no legacy
			// window-maintenance write is required.
			if sharedPurchase != nil {
				if validateErr := subscriptionService.ValidateSharedPurchase(sharedPurchase, 0); validateErr != nil {
					if sharedConcurrencyPlan != nil &&
						sharedConcurrencyPlan.AllowBalanceTopup &&
						isSharedSubscriptionQuotaError(validateErr) {
						// Quota drained but the purchase allows spilling over to account
						// balance: drop the entitlement and fall through to the balance check.
						subscription = nil
						sharedPurchase = nil
						c.Set(string(ContextKeySharedSubscription), nil)
					} else {
						code := "SUBSCRIPTION_INVALID"
						status := 403
						if isSharedSubscriptionQuotaError(validateErr) {
							code = "USAGE_LIMIT_EXCEEDED"
							status = 429
						}
						AbortWithError(c, status, code, validateErr.Error())
						return
					}
				}
			}
			if subscription == nil {
				// No purchase snapshot (or balance fallback allowed): check balance.
				if apiKeyBalanceBelowAuthThreshold(apiKey.User.Balance, cfg) {
					AbortWithError(c, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance")
					return
				}
			}
		}

		// ?? 7. ????? ? Next ?????????????????????????????????????

		if subscription != nil {
			c.Set(string(ContextKeySubscription), subscription)
		}
		if sharedConcurrencyPlan != nil {
			c.Request = c.Request.WithContext(service.WithSubscriptionConcurrencyPlan(c.Request.Context(), sharedConcurrencyPlan))
		}
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		if !billingInfoRequest {
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		}

		c.Next()
	}
}

func apiKeyHeadersTooLarge(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return len(c.GetHeader("Authorization")) > maxAPIKeyAuthorizationHeaderBytes ||
		len(c.GetHeader("x-api-key")) > service.MaxAPIKeyCredentialBytes ||
		len(c.GetHeader("x-goog-api-key")) > service.MaxAPIKeyCredentialBytes
}

func hasAPIKeyCredentialInput(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetHeader("Authorization") != "" ||
		c.GetHeader("x-api-key") != "" ||
		c.GetHeader("x-goog-api-key") != ""
}

func abortWithAPIKeyQuotaError(c *gin.Context) {
	const message = "API key ?????"
	if isOpenAICompatibleAPIKeyRequest(c) {
		abortWithOpenAIQuotaError(c, http.StatusTooManyRequests, message)
		return
	}
	AbortWithError(c, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", message)
}

func isOpenAICompatibleAPIKeyRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}

	path := strings.TrimRight(c.Request.URL.Path, "/")
	for _, root := range []string{
		"/v1/responses",
		"/openai/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func isAsyncImageTaskRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(path, "/v1/images/tasks/") || strings.HasPrefix(path, "/images/tasks/")
}

// GetAPIKeyFromContext ???????API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey ?????? API Key?? Ops ???????????????
// ? ContextKeyAPIKey ?????????????????????? handler?
// ???????????????
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey ?? Ops ????????? API Key?
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext ???????????
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}

// apiKeyBalanceBelowAuthThreshold ??????????????????<=0?????
// MinimumBalanceReserve ??? billing-cache ???????????????????
// ????????????????0 < balance < reserve ???????????? 403?
func apiKeyBalanceBelowAuthThreshold(balance float64, _ *config.Config) bool {
	return balance <= 0
}

func abortIfAPIKeyGroupUnavailable(c *gin.Context, apiKey *service.APIKey) bool {
	code, message, ok := validateAPIKeyGroupAvailable(apiKey)
	if ok {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	if code == "GROUP_DELETED" {
		MarkIngressRejected(c, IngressRejectGroupDeleted)
	} else {
		MarkIngressRejected(c, IngressRejectGroupDisabled)
	}
	AbortWithError(c, 403, code, message)
	return true
}

func abortIfAPIKeyGroupNotAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if validateAPIKeyGroupAllowed(apiKey) {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	MarkIngressRejected(c, IngressRejectGroupNotAllowed)
	AbortWithError(c, 403, "GROUP_NOT_ALLOWED", "API Key ????????????????")
	return true
}

func validateAPIKeyGroupAllowed(apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil || apiKey.Group == nil {
		return true
	}
	group := apiKey.Group
	if group.IsSubscriptionType() {
		return true
	}
	return apiKey.User.CanBindGroup(group.ID, group.IsExclusive)
}

func validateAPIKeyGroupAvailable(apiKey *service.APIKey) (string, string, bool) {
	if apiKey == nil || apiKey.GroupID == nil {
		return "", "", true
	}
	group := apiKey.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "API Key ???????", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "API Key ???????", false
	}
	return "", "", true
}
