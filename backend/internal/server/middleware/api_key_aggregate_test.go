//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func aggregateFixture() *service.APIKey {
	a, b := int64(10), int64(20)
	return &service.APIKey{ID: 100, UserID: 7, Key: "root", KeyKind: service.APIKeyKindAggregate,
		User: &service.User{ID: 7, Status: service.StatusActive},
		Members: []*service.APIKey{
			{ID: 1, UserID: 7, Status: service.StatusActive, GroupID: &a, Group: &service.Group{ID: a, Status: service.StatusActive}},
			{ID: 2, UserID: 7, Status: service.StatusActive, GroupID: &b, Group: &service.Group{ID: b, Status: service.StatusActive}},
		}}
}

func TestAggregateRoutingPreservesIdentityAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := aggregateFixture()
	body := `{"model":"second","messages":[],"stream":true}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(aggregateCheckerContextKey, aggregateModelChecker(func(_ context.Context, g *service.Group, model string) bool { return g.ID == 20 && model == "second" }))
	key, ok := routeAggregateAPIKey(c, root, nil, &config.Config{})
	require.True(t, ok)
	require.Equal(t, root.ID, key.ID)
	require.Equal(t, root.Key, key.Key)
	require.Equal(t, int64(2), key.RoutedMemberKeyID)
	require.Equal(t, int64(20), *key.GroupID)
	require.Nil(t, root.GroupID)
	require.NotSame(t, root.User, key.User)
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(restored))
}

func TestAggregateRoutingRejectsUnsafeMembersAndAmbiguousModels(t *testing.T) {
	for _, reason := range []string{"ownership", "quota", "expired", "group-access", "ip", "nested", "model-ambiguity"} {
		t.Run(reason, func(t *testing.T) {
			root := aggregateFixture()
			root.Members = root.Members[:1]
			member := root.Members[0]
			body := `{"model":"first"}`
			switch reason {
			case "ownership":
				member.UserID = 8
			case "quota":
				member.Quota = 1
				member.QuotaUsed = 1
			case "expired":
				member.Status = service.StatusAPIKeyExpired
			case "group-access":
				member.Group.IsExclusive = true
			case "nested":
				member.KeyKind = service.APIKeyKindAggregate
			case "ip":
				member.IPWhitelist = []string{"10.0.0.0/8"}
				member.CompiledIPWhitelist = ip.CompileIPRules(member.IPWhitelist)
			case "model-ambiguity":
				body = `{"model":"first","MODEL":"second"}`
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(aggregateCheckerContextKey, aggregateModelChecker(func(_ context.Context, _ *service.Group, model string) bool { return model == "first" }))
			_, ok := routeAggregateAPIKey(c, root, nil, &config.Config{})
			require.False(t, ok)
			require.True(t, c.IsAborted())
		})
	}
}

func TestAggregateRoutingCatalogAndUnsupportedSurfaces(t *testing.T) {
	for _, path := range []string{"/v1/models", "/v1/usage", "/v1/sub2api/billing"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		root := aggregateFixture()
		root.Members[0].UserID = 8
		key, ok := routeAggregateAPIKey(c, root, nil, &config.Config{})
		require.True(t, ok)
		require.Nil(t, key.GroupID)
		require.Len(t, key.Members, 1)
	}
	for _, path := range []string{"/v1/images/generations/async", "/v1/images/batches"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"first"}`))
		_, ok := routeAggregateAPIKey(c, aggregateFixture(), nil, &config.Config{})
		require.False(t, ok)
	}
}
