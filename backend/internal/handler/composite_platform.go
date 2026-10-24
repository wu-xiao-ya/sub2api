package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// effectiveAPIKeyPlatform 返回本次请求实际生效的平台：显式强制平台优先，
// 否则取 API Key 所属分组平台。
func effectiveAPIKeyPlatform(c *gin.Context, apiKey *service.APIKey) string {
	if platform, ok := middleware.GetForcePlatformFromContext(c); ok && platform != "" {
		return platform
	}
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}
