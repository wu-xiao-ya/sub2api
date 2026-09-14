package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPerformanceHandlerAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, detail := range []bool{false, true} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/performance", nil)
		(&AvailableChannelHandler{}).performance(c, detail)
		require.Equal(t, http.StatusUnauthorized, writer.Code)
	}
}

func TestPerformanceScopesMatchVisibleChannels(t *testing.T) {
	channels := []service.AvailableChannel{
		{Name: "public", Status: service.StatusActive, Groups: []service.AvailableGroupRef{{ID: 1, Name: "visible", Platform: "openai"}, {ID: 2, Name: "hidden", Platform: "anthropic"}},
			SupportedModels: []service.SupportedModel{{Name: "gpt-exact", Platform: "openai"}, {Name: "gpt-exact-discount", Platform: "openai"}, {Name: "secret", Platform: "anthropic"}}},
		{Name: "disabled", Status: "disabled", Groups: []service.AvailableGroupRef{{ID: 1, Platform: "openai"}}, SupportedModels: []service.SupportedModel{{Name: "gpt-exact", Platform: "openai"}}},
	}
	scopes := availablePerformanceScopes(channels, map[int64]struct{}{1: {}})
	require.Len(t, scopes, 2)
	require.Equal(t, "public\x00openai\x00gpt-exact", scopes[0].Key)
	for _, scope := range scopes {
		require.Equal(t, map[int64]string{1: "visible"}, scope.Groups)
		require.NotEqual(t, "secret", scope.Model)
	}
	require.Empty(t, availablePerformanceScopes(channels, map[int64]struct{}{}))
}

func TestPerformanceDimensionsDoNotInventStandardOrKeepBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	setPerformanceDimensions(c, []byte(`{"messages":[{"content":"private text"}]} `))
	require.Empty(t, c.GetString(performanceTierKey))
	require.Empty(t, c.GetString(performanceEffortKey))
	setPerformanceDimensions(c, []byte(`{"service_tier":"priority","reasoning":{"effort":"high"},"messages":[{"content":"private text"}]} `))
	require.Equal(t, "priority", c.GetString(performanceTierKey))
	require.Equal(t, "high", c.GetString(performanceEffortKey))
	require.Len(t, c.Keys, 2)
}
