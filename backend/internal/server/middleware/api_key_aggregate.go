package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type aggregateModelChecker func(context.Context, *service.Group, string) bool

const aggregateCheckerContextKey = "aggregate.model-checker"

// WithAggregateRouting supplies the native auth middleware with the gateway's
// model catalog. Selection runs inside auth, before subscription/group checks.
func WithAggregateRouting(auth gin.HandlerFunc, checker func(context.Context, *service.Group, string) bool) gin.HandlerFunc {
	return func(c *gin.Context) { c.Set(aggregateCheckerContextKey, aggregateModelChecker(checker)); auth(c) }
}

func aggregateInspectionRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}
	switch strings.TrimRight(c.Request.URL.Path, "/") {
	case "/v1/models", "/models", "/backend-api/codex/models", "/v1/usage", "/v1/sub2api/billing":
		return true
	}
	return isAsyncImageTaskRead(c.Request.Method, c.Request.URL.Path)
}

func aggregateMemberAccessible(c *gin.Context, root, member *service.APIKey, cfg *config.Config) bool {
	if root == nil || member == nil || member.UserID != root.UserID || !member.IsMemberUsable() {
		return false
	}
	// Always evaluate group access against the authenticated user's current grants.
	candidate := *member
	candidate.User = root.User
	if !validateAPIKeyGroupAllowed(&candidate) {
		return false
	}
	if len(member.IPWhitelist) != 0 || len(member.IPBlacklist) != 0 {
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL()), member.CompiledIPWhitelist, member.CompiledIPBlacklist)
		if !allowed {
			return false
		}
	}
	return true
}

func routeAggregateAPIKey(c *gin.Context, root *service.APIKey, keys *service.APIKeyService, cfg *config.Config) (*service.APIKey, bool) {
	// Catalogs and key-owned historical resources keep the root identity and never
	// fall through to ungrouped scheduling. Filter catalog members by the same ACL.
	if aggregateInspectionRequest(c) {
		clone := *root
		clone.Members = nil
		for _, member := range root.Members {
			if aggregateMemberAccessible(c, root, member, cfg) {
				clone.Members = append(clone.Members, member)
			}
		}
		return &clone, true
	}
	// New background image jobs currently settle from their persisted root
	// snapshot. Require a member key until that format records aggregate routing.
	if strings.Contains(c.Request.URL.Path, "/images/batches") || strings.HasSuffix(c.Request.URL.Path, "/async") {
		AbortWithError(c, http.StatusBadRequest, "AGGREGATE_UNSUPPORTED_ENDPOINT", "Use a member key for background image jobs")
		return nil, false
	}
	// A Responses socket can change models after its HTTP handshake. Selecting
	// one member from a query parameter cannot safely cover the whole connection.
	if isResponsesWebSocketRoute(c) {
		AbortWithError(c, http.StatusBadRequest, "AGGREGATE_UNSUPPORTED_ENDPOINT", "Use HTTP Responses streaming or a member key for WebSocket requests")
		return nil, false
	}
	var models []string
	if model := groupModelAllowlistModelFromParams(c); model != "" {
		models = []string{model}
	} else {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			var ok bool
			models, ok = groupModelAllowlistModelsFromBody(c)
			if !ok {
				return nil, false
			}
		}
	}
	if len(models) == 0 {
		if model := strings.TrimSpace(c.Query("model")); model != "" {
			models = []string{model}
		}
	}
	if len(models) == 0 {
		AbortWithError(c, http.StatusBadRequest, "AGGREGATE_MODEL_REQUIRED", service.ErrAggregateModelRequired.Error())
		return nil, false
	}
	value, _ := c.Get(aggregateCheckerContextKey)
	checker, _ := value.(aggregateModelChecker)
	if checker != nil {
		for _, member := range root.Members {
			if !aggregateMemberAccessible(c, root, member, cfg) {
				continue
			}
			matches := true
			for _, model := range models {
				if !checker(c.Request.Context(), member.Group, model) {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			routed := service.BindAggregateRoute(root, member)
			// Never mutate an auth-cache-owned User pointer when selecting a group.
			if root.User != nil {
				user := *root.User
				user.UserGroupRPMOverride = nil
				routed.User = &user
			}
			return routed, true
		}
	}
	AbortWithError(c, http.StatusBadRequest, "AGGREGATE_NO_MATCHING_MEMBER", service.ErrAggregateNoMatchingMember.Error())
	return nil, false
}
