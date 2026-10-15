package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageCostHandlerRepoStub struct {
	values map[string]string
}

func (r *imageCostHandlerRepoStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r *imageCostHandlerRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *imageCostHandlerRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *imageCostHandlerRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *imageCostHandlerRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *imageCostHandlerRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *imageCostHandlerRepoStub) Delete(context.Context, string) error {
	return nil
}

func newImageCostHandlerTest(t *testing.T) (*SettingHandler, *imageCostHandlerRepoStub) {
	t.Helper()
	repo := &imageCostHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

func TestImageUpstreamCostHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo := newImageCostHandlerTest(t)

	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/image-upstream-cost", nil)
	handler.GetImageUpstreamCost(getContext)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Contains(t, getRecorder.Body.String(), `"cost_per_image":0.001`)

	body, err := json.Marshal(map[string]float64{"cost_per_image": 0.002})
	require.NoError(t, err)
	putRecorder := httptest.NewRecorder()
	putContext, _ := gin.CreateTestContext(putRecorder)
	putContext.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings/image-upstream-cost",
		bytes.NewReader(body),
	)
	putContext.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateImageUpstreamCost(putContext)
	require.Equal(t, http.StatusOK, putRecorder.Code)
	require.Equal(t, "0.002", repo.values[service.SettingKeyImageUpstreamCostPerImage])
	require.Contains(t, putRecorder.Body.String(), `"cost_per_image":0.002`)
}

func TestImageUpstreamCostHandlerUpdatesAccountOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo := newImageCostHandlerTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings/image-upstream-cost",
		bytes.NewBufferString(`{"account_overrides":[{"account_id":68,"cost_per_image":0.1},{"account_id":69,"cost_per_image":0.01}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateImageUpstreamCost(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"68":0.1,"69":0.01}`, repo.values[service.SettingKeyImageUpstreamCostByAccount])
	require.Contains(t, recorder.Body.String(), `"account_id":68`)
	require.Contains(t, recorder.Body.String(), `"account_id":69`)
}

func TestImageUpstreamCostHandlerRejectsMissingValue(t *testing.T) {
	handler, _ := newImageCostHandlerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings/image-upstream-cost",
		bytes.NewBufferString(`{}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateImageUpstreamCost(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
