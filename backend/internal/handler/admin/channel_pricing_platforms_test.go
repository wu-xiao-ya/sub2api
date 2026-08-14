package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSyncPricingModelsCompatiblePlatformsIncludeMaintainedDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ChannelHandler{pricingService: service.NewPricingService(nil, nil)}
	router := gin.New()
	router.GET("/channels/pricing/sync-models", handler.SyncPricingModels)

	tests := []struct {
		platform string
		model    string
	}{
		{platform: service.PlatformDeepSeek, model: "deepseek-chat"},
		{platform: service.PlatformKimi, model: "kimi-k2.6"},
		{platform: service.PlatformGLM, model: "glm-4.7"},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"/channels/pricing/sync-models?platform="+tt.platform,
				nil,
			)
			responseRecorder := httptest.NewRecorder()
			router.ServeHTTP(responseRecorder, request)

			require.Equal(t, http.StatusOK, responseRecorder.Code)
			var body struct {
				Data struct {
					Models []string `json:"models"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &body))
			require.Contains(t, body.Data.Models, tt.model)
		})
	}
}

func TestMergeChannelPricingModelNamesKeepsPrimaryOrderAndDeduplicatesRegular(t *testing.T) {
	require.Equal(t,
		[]string{"kimi-k2.6", "moonshot-v1-8k", "moonshot-v1-32k"},
		mergeChannelPricingModelNames(
			[]string{"kimi-k2.6", "moonshot-v1-8k"},
			[]string{"KIMI-K2.6", "moonshot-v1-32k"},
		),
	)
}
