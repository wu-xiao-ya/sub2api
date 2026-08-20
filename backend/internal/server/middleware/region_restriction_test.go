package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegionRestriction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	effectiveAt := time.Date(2026, 8, 24, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	cfg := config.RegionRestrictionConfig{
		Enabled:          true,
		CountryHeader:    "CF-IPCountry",
		BlockedCountries: []string{"CN"},
		EffectiveAt:      effectiveAt.Format(time.RFC3339),
		RestrictedPath:   "/region-restricted",
		ExemptPaths:      []string{"/health", "/api/v1/payment/webhook"},
	}

	tests := []struct {
		name            string
		now             time.Time
		method          string
		path            string
		accept          string
		country         string
		wantStatus      int
		wantLocation    string
		wantBody        string
		wantNextHandler bool
	}{
		{
			name:            "before effective date allows request",
			now:             effectiveAt.Add(-time.Second),
			method:          http.MethodPost,
			path:            "/v1/responses",
			country:         "CN",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "non blocked country allows request",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/v1/responses",
			country:         "US",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "hong kong remains supported",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/v1/responses",
			country:         "HK",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "macao remains supported",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/v1/responses",
			country:         "MO",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "taiwan remains supported",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/v1/responses",
			country:         "TW",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "missing country header fails open",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/v1/responses",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:         "browser navigation redirects to restriction page",
			now:          effectiveAt,
			method:       http.MethodGet,
			path:         "/dashboard",
			accept:       "text/html,application/xhtml+xml",
			country:      "cn",
			wantStatus:   http.StatusTemporaryRedirect,
			wantLocation: "/region-restricted?country=CN",
		},
		{
			name:       "api request receives structured 403",
			now:        effectiveAt,
			method:     http.MethodPost,
			path:       "/v1/responses",
			country:    "CN",
			wantStatus: http.StatusForbidden,
			wantBody:   `{"error":{"type":"permission_error","code":"REGION_RESTRICTED","message":"This service is not available in your region."}}`,
		},
		{
			name:       "anthropic request receives anthropic error",
			now:        effectiveAt,
			method:     http.MethodPost,
			path:       "/v1/messages",
			country:    "CN",
			wantStatus: http.StatusForbidden,
			wantBody:   `{"type":"error","error":{"type":"permission_error","message":"This service is not available in your region."}}`,
		},
		{
			name:       "gemini request receives google error",
			now:        effectiveAt,
			method:     http.MethodPost,
			path:       "/v1beta/models/gemini:generateContent",
			country:    "CN",
			wantStatus: http.StatusForbidden,
			wantBody:   `{"error":{"code":403,"message":"This service is not available in your region.","status":"PERMISSION_DENIED"}}`,
		},
		{
			name:       "console api receives platform error",
			now:        effectiveAt,
			method:     http.MethodGet,
			path:       "/api/v1/auth/me",
			country:    "CN",
			wantStatus: http.StatusForbidden,
			wantBody:   `{"code":"REGION_RESTRICTED","message":"This service is not available in your region."}`,
		},
		{
			name:            "restriction page remains accessible",
			now:             effectiveAt,
			method:          http.MethodGet,
			path:            "/region-restricted",
			accept:          "text/html",
			country:         "CN",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "static background remains accessible",
			now:             effectiveAt,
			method:          http.MethodGet,
			path:            "/region-restricted-bg.png",
			country:         "CN",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
		{
			name:            "payment webhook remains accessible",
			now:             effectiveAt,
			method:          http.MethodPost,
			path:            "/api/v1/payment/webhook/easypay",
			country:         "CN",
			wantStatus:      http.StatusNoContent,
			wantNextHandler: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(newRegionRestriction(cfg, func() time.Time { return tt.now }))
			nextHandlerCalled := false
			router.Any("/*path", func(c *gin.Context) {
				nextHandlerCalled = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
			}
			if tt.country != "" {
				request.Header.Set("CF-IPCountry", tt.country)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Equal(t, tt.wantLocation, recorder.Header().Get("Location"))
			if tt.wantBody != "" {
				require.JSONEq(t, tt.wantBody, recorder.Body.String())
			}
			if tt.wantNextHandler {
				require.Empty(t, recorder.Header().Get("X-Region-Restricted"))
			}
			require.Equal(t, tt.wantNextHandler, nextHandlerCalled)
		})
	}
}

func TestRegionRestrictionDisabled(t *testing.T) {
	router := gin.New()
	router.Use(RegionRestriction(config.RegionRestrictionConfig{Enabled: false}))
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("CF-IPCountry", "CN")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}
