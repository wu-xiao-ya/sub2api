package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type aggregateChecker map[int64][]string

func (c aggregateChecker) GroupSupportsRequestedModel(_ context.Context, group *service.Group, requestedModel string) bool {
	if group == nil {
		return false
	}
	for _, model := range c[group.ID] {
		if model == requestedModel {
			return true
		}
	}
	return false
}

func TestResolveAggregateAPIKeyRoutesByModelOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gidGPT, gidClaude := int64(1), int64(2)
	gptKey := &service.APIKey{
		ID:      11,
		UserID:  8,
		Status:  service.StatusActive,
		KeyKind: service.APIKeyKindGroup,
		GroupID: &gidGPT,
		Group:   &service.Group{ID: gidGPT, Name: "gpt", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard},
	}
	claudeKey := &service.APIKey{
		ID:      12,
		UserID:  8,
		Status:  service.StatusActive,
		KeyKind: service.APIKeyKindGroup,
		GroupID: &gidClaude,
		Group:   &service.Group{ID: gidClaude, Name: "claude", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard},
	}
	aggregate := &service.APIKey{
		ID:      99,
		UserID:  8,
		Status:  service.StatusActive,
		KeyKind: service.APIKeyKindAggregate,
		User:    &service.User{ID: 8, Status: service.StatusActive, Role: service.RoleUser, Balance: 10, Concurrency: 5},
		Members: []*service.APIKey{gptKey, claudeKey},
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), aggregate)
		c.Next()
	})
	router.Use(ResolveAggregateAPIKey(aggregateChecker{
		gidGPT:    {"gpt-5.4"},
		gidClaude: {"claude-sonnet-4-5"},
	}, nil, &config.Config{RunMode: config.RunModeSimple}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		key, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"member": key.RoutedMemberKeyID, "platform": key.Group.Platform})
	})

	body, err := json.Marshal(map[string]any{"model": "claude-sonnet-4-5"})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(12), resp["member"])
	require.Equal(t, service.PlatformAnthropic, resp["platform"])
}

func TestRejectAggregateAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 1, KeyKind: service.APIKeyKindAggregate, Status: service.StatusActive})
		c.Next()
	})
	router.Use(RejectAggregateAPIKey())
	router.POST("/v1beta/models", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models", bytes.NewReader([]byte("{}")))
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPeekRequestModelRestoresBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte("{\"model\":\"gpt-5.4\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	model, err := peekRequestModel(c)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", model)
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, payload, restored)
}

func TestRequireGroupAssignmentAllowsUnresolvedAggregate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 1, KeyKind: service.APIKeyKindAggregate, Status: service.StatusActive})
		c.Next()
	})
	router.Use(RequireGroupAssignment(nil, AnthropicErrorWriter))
	router.GET("/v1/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
