package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pluginTestAccountRepo struct {
	AccountRepository
	account *Account
}

func (r pluginTestAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

type pluginTestTransport struct {
	roundTrip func(*http.Request, *Account) (*http.Response, error)
}

func (p pluginTestTransport) ProviderAccountModels(context.Context, *Account) ([]string, error) {
	return []string{"deepseek-v4-flash", "deepseek-v4-pro"}, nil
}

func (p pluginTestTransport) RoundTripProvider(_ context.Context, req *http.Request, _ string, account *Account) (*http.Response, error) {
	return p.roundTrip(req, account)
}

func (p pluginTestTransport) RoundTripOpenAIOAuth(context.Context, *http.Request, string, *Account) (*http.Response, bool, error) {
	return nil, false, nil
}

func TestPluginAccountPickerAndConnectionUseBoundCatalog(t *testing.T) {
	account := &Account{
		ID: 284, Platform: PlatformDeepseek, Type: AccountTypePlugin,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"flash": "deepseek-v4-flash", "invalid": "other"},
		},
	}
	calls := 0
	svc := &AccountTestService{
		accountRepo: pluginTestAccountRepo{account: account},
		pluginManager: pluginTestTransport{roundTrip: func(req *http.Request, got *Account) (*http.Response, error) {
			calls++
			require.Equal(t, account.ID, got.ID)
			require.Equal(t, "https://plugin.invalid/v1/chat/completions", req.URL.String())
			require.Empty(t, req.Header.Get("Authorization"))
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`, string(body))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n",
			))}, nil
		}},
	}
	models, err := svc.FetchPluginAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "flash", models[0].ID)
	require.Equal(t, "flash", models[0].DisplayName)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	require.NoError(t, svc.TestAccountConnection(c, account.ID, "flash", "hello", ""))
	require.Contains(t, rec.Body.String(), `"text":"hi"`)
	require.Contains(t, rec.Body.String(), `"success":true`)

	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	require.ErrorContains(t, svc.TestAccountConnection(c, account.ID, "deepseek-v4-pro", "hello", ""), "not available")
	require.Equal(t, 1, calls)
}
