package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestImportVerifiesIdentityKeepsExistingReferenceAndRedactsTokens(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer imported-secret", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v2/plugin/login/account":
			_, _ = io.WriteString(w, `{"code":0,"data":{"uid":"verified","domain":"codebuddy.cn"}}`)
		case "/v2/enterprises/personal/models":
			require.Equal(t, "verified", r.Header.Get("X-User-Id"))
			_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"deepseek-v4-flash"},{"id":"other"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := newServer()
	s.billing, s.chat = upstream.URL, upstream.URL
	s.now = func() time.Time { return time.Unix(1800000000, 0) }
	ref := identityRef("verified")
	config := configuration{Accounts: map[string]credential{ref: {UID: "verified", AccessToken: "old-secret", Domain: "codebuddy.cn"}}}
	result, err := s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{
		Action: "import", ConfigJson: rawConfig(config), SessionId: `[
			{"access_token":"imported-secret","uid":"untrusted","domain":"codebuddy.cn","expiresAt":1800003600000},
			{"access_token":"foreign-secret","domain":"codebuddy.ai","expiresAt":1800003600000}
		]`,
	})
	require.NoError(t, err)
	require.True(t, result.ConfigChanged)
	require.NotContains(t, string(result.PublicJson), "secret")
	require.Contains(t, string(result.PublicJson), ref)
	require.Contains(t, string(result.PublicJson), "invalid_credentials_or_region")
	var saved configuration
	require.NoError(t, json.Unmarshal(result.ConfigJson, &saved))
	require.Len(t, saved.Accounts, 1)
	require.Equal(t, "imported-secret", saved.Accounts[ref].AccessToken)
	require.Equal(t, []string{"deepseek-v4-flash", "other"}, saved.Accounts[ref].Models)
}
