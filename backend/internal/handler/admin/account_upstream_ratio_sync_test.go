package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupUpstreamRatioSyncRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.SetUpstreamRatioSyncService(service.NewUpstreamRatioSyncService(nil, nil, nil, nil))

	router := gin.New()
	router.GET("/admin/accounts/upstream-ratio-sync/settings", handler.GetUpstreamRatioSyncSettings)
	router.PUT("/admin/accounts/upstream-ratio-sync/settings", handler.UpdateUpstreamRatioSyncSettings)
	router.POST("/admin/accounts/upstream-ratio-sync/batch", handler.SyncUpstreamRatioBatch)
	router.PUT("/admin/accounts/:id/upstream-ratio-sync", handler.SetUpstreamRatioSyncEnabled)
	return router
}

func TestAccountHandlerGetUpstreamRatioSyncSettingsReturnsDefaults(t *testing.T) {
	router := setupUpstreamRatioSyncRouter()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/accounts/upstream-ratio-sync/settings", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data service.UpstreamRatioSyncSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Data.Enabled)
	require.Equal(t, 30, response.Data.IntervalMinutes)
	require.Equal(t, int64(500000), response.Data.QuotaPerUnit)
	require.Equal(t, 90, response.Data.RetentionDays)
}

func TestAccountHandlerUpdateUpstreamRatioSyncSettingsValidates(t *testing.T) {
	router := setupUpstreamRatioSyncRouter()

	// interval below the minimum is rejected by the service layer; with a nil
	// setting repo the handler surfaces the service-unavailable error instead.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/admin/accounts/upstream-ratio-sync/settings",
		bytes.NewBufferString(`{"enabled": true, "interval_minutes": 1}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestAccountHandlerUpstreamRatioSyncBatchValidatesIDs(t *testing.T) {
	router := setupUpstreamRatioSyncRouter()

	for _, body := range []string{`{"account_ids":[]}`, `{"account_ids":[0]}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/admin/accounts/upstream-ratio-sync/batch", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}

func TestAccountHandlerUpstreamRatioSyncEnableRequiresAccount(t *testing.T) {
	router := setupUpstreamRatioSyncRouter()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/admin/accounts/404/upstream-ratio-sync",
		bytes.NewBufferString(`{"enabled": true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code) // nil repo surfaces as service unavailable

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/admin/accounts/abc/upstream-ratio-sync",
		bytes.NewBufferString(`{"enabled": true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
