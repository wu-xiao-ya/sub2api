package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type channelMonitorUserSettingsRepoStub struct {
	values map[string]string
}

func (s *channelMonitorUserSettingsRepoStub) Get(context.Context, string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *channelMonitorUserSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s *channelMonitorUserSettingsRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *channelMonitorUserSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (s *channelMonitorUserSettingsRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *channelMonitorUserSettingsRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *channelMonitorUserSettingsRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestChannelMonitorUserFeatureEnabledKeepsLegacyViewReadableInHybridMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{service.ChannelMonitorModeV1, service.ChannelMonitorModeV2} {
		t.Run(mode, func(t *testing.T) {
			settingService := service.NewSettingService(&channelMonitorUserSettingsRepoStub{
				values: map[string]string{
					service.SettingKeyChannelMonitorEnabled: "true",
					service.SettingKeyChannelMonitorMode:    mode,
				},
			}, &config.Config{})
			h := NewChannelMonitorUserHandler(nil, settingService)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/api/v1/channel-monitors", nil)

			require.True(t, h.featureEnabled(ctx))
		})
	}
}

func TestChannelMonitorUserFeatureEnabledHonorsGlobalDisable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingService := service.NewSettingService(&channelMonitorUserSettingsRepoStub{
		values: map[string]string{
			service.SettingKeyChannelMonitorEnabled: "false",
			service.SettingKeyChannelMonitorMode:    service.ChannelMonitorModeV2,
		},
	}, &config.Config{})
	h := NewChannelMonitorUserHandler(nil, settingService)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/api/v1/channel-monitors", nil)

	require.False(t, h.featureEnabled(ctx))
}
