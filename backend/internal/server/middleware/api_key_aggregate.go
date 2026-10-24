package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func RejectAggregateAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || !apiKey.IsAggregate() {
			c.Next()
			return
		}
		abortWithApplicationError(c, service.ErrAggregateUnsupportedEndpoint)
	}
}

func ResolveAggregateAPIKey(checker service.AggregateModelSupportChecker, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || !apiKey.IsAggregate() {
			c.Next()
			return
		}
		if skipAggregateModelRouting(c) {
			c.Next()
			return
		}

		model, err := peekRequestModel(c)
		if err != nil {
			AbortWithError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}
		member, err := service.ResolveAggregateMember(c.Request.Context(), apiKey, model, checker)
		if err != nil {
			abortWithApplicationError(c, err)
			return
		}
		if abortIfAPIKeyGroupUnavailable(c, member) {
			return
		}
		routed := service.BindAggregateRoute(apiKey, member)
		if routed.User == nil {
			routed.User = apiKey.User
		}
		if abortIfAPIKeyGroupNotAllowed(c, routed) {
			return
		}
		if member.IsExpired() {
			AbortWithError(c, http.StatusForbidden, "API_KEY_EXPIRED", "API key 已过期")
			return
		}
		if member.IsQuotaExhausted() {
			abortWithAPIKeyQuotaError(c)
			return
		}

		c.Set(string(ContextKeyAPIKey), routed)
		setGroupContext(c, routed.Group)

		if cfg != nil && cfg.RunMode == config.RunModeSimple {
			c.Next()
			return
		}

		if !attachRoutedAggregateEntitlements(c, routed, subscriptionService, cfg) {
			return
		}
		c.Next()
	}
}

func skipAggregateModelRouting(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	switch path {
	case "/v1/models", "/models", "/v1/usage", "/v1/sub2api/billing":
		return true
	}
	if strings.HasPrefix(path, "/v1/images/tasks/") || strings.HasPrefix(path, "/images/tasks/") {
		return true
	}
	return false
}

func peekRequestModel(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil {
		return "", nil
	}
	if model := strings.TrimSpace(c.Query("model")); model != "" {
		return model, nil
	}
	if c.Request.Body == nil {
		return "", nil
	}
	body, err := io.ReadAll(c.Request.Body)
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return "", nil
	}
	var payload struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil
	}
	if len(payload.Model) == 0 {
		return "", nil
	}
	var model string
	if err := json.Unmarshal(payload.Model, &model); err != nil {
		return "", nil
	}
	return strings.TrimSpace(model), nil
}

func attachRoutedAggregateEntitlements(c *gin.Context, apiKey *service.APIKey, subscriptionService *service.SubscriptionService, cfg *config.Config) bool {
	var subscription *service.UserSubscription
	var sharedPurchase *service.SharedSubscriptionEntitlement
	var sharedConcurrencyPlan *service.SubscriptionConcurrencyPlan
	isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
	planLoaded := false
	if apiKey.Group != nil && subscriptionService != nil {
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
				return false
			}
		}
	}
	if isSubscriptionType && !planLoaded {
		AbortWithError(c, http.StatusForbidden, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group")
		return false
	}
	if sharedPurchase != nil {
		if validateErr := subscriptionService.ValidateSharedPurchase(sharedPurchase, 0); validateErr != nil {
			if sharedConcurrencyPlan != nil &&
				sharedConcurrencyPlan.AllowBalanceTopup &&
				isSharedSubscriptionQuotaError(validateErr) {
				subscription = nil
				sharedPurchase = nil
				c.Set(string(ContextKeySharedSubscription), nil)
			} else {
				code := "SUBSCRIPTION_INVALID"
				status := http.StatusForbidden
				if isSharedSubscriptionQuotaError(validateErr) {
					code = "USAGE_LIMIT_EXCEEDED"
					status = http.StatusTooManyRequests
				}
				AbortWithError(c, status, code, validateErr.Error())
				return false
			}
		}
	}
	if subscription == nil {
		if apiKey.User != nil && apiKeyBalanceBelowAuthThreshold(apiKey.User.Balance, cfg) {
			AbortWithError(c, http.StatusForbidden, "INSUFFICIENT_BALANCE", "Insufficient account balance")
			return false
		}
	}
	if subscription != nil {
		c.Set(string(ContextKeySubscription), subscription)
	}
	if sharedConcurrencyPlan != nil {
		c.Request = c.Request.WithContext(service.WithSubscriptionConcurrencyPlan(c.Request.Context(), sharedConcurrencyPlan))
	}
	return true
}

func abortWithApplicationError(c *gin.Context, err error) {
	var appErr *pkgerrors.ApplicationError
	if errors.As(err, &appErr) {
		AbortWithError(c, int(appErr.Code), appErr.Reason, appErr.Message)
		return
	}
	AbortWithError(c, http.StatusBadRequest, "AGGREGATE_ROUTE_FAILED", err.Error())
}
