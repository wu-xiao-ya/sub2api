package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

const (
	regionRestrictedCode = "REGION_RESTRICTED"
)

// RegionRestriction uses a trusted CDN country header to block configured
// regions. A missing country header fails open for local checks and development.
func RegionRestriction(cfg config.RegionRestrictionConfig) gin.HandlerFunc {
	return newRegionRestrictionWithRuntime(cfg, time.Now, nil)
}

func newRegionRestriction(cfg config.RegionRestrictionConfig, now func() time.Time) gin.HandlerFunc {
	return newRegionRestrictionWithRuntime(cfg, now, nil)
}

// RegionRestrictionWithRuntime allows the persisted admin switch to override
// the startup configuration without rebuilding the router or restarting.
func RegionRestrictionWithRuntime(
	cfg config.RegionRestrictionConfig,
	enabled func(context.Context) bool,
) gin.HandlerFunc {
	return newRegionRestrictionWithRuntime(cfg, time.Now, enabled)
}

func newRegionRestrictionWithRuntime(
	cfg config.RegionRestrictionConfig,
	now func() time.Time,
	runtimeEnabled func(context.Context) bool,
) gin.HandlerFunc {

	countryHeader := strings.TrimSpace(cfg.CountryHeader)
	if countryHeader == "" {
		countryHeader = "CF-IPCountry"
	}

	restrictedPath := normalizeRegionPath(cfg.RestrictedPath)
	if restrictedPath == "/" {
		restrictedPath = "/region-restricted"
	}

	blockedCountries := make(map[string]struct{}, len(cfg.BlockedCountries))
	for _, country := range cfg.BlockedCountries {
		if normalized := strings.ToUpper(strings.TrimSpace(country)); normalized != "" {
			blockedCountries[normalized] = struct{}{}
		}
	}

	exemptPaths := make([]string, 0, len(cfg.ExemptPaths))
	for _, exemptPath := range cfg.ExemptPaths {
		if normalized := normalizeRegionPath(exemptPath); normalized != "/" {
			exemptPaths = append(exemptPaths, normalized)
		}
	}

	var effectiveAt time.Time
	if raw := strings.TrimSpace(cfg.EffectiveAt); raw != "" {
		effectiveAt, _ = time.Parse(time.RFC3339, raw)
	}

	return func(c *gin.Context) {
		enabled := cfg.Enabled
		if runtimeEnabled != nil {
			enabled = runtimeEnabled(c.Request.Context())
		}
		if !enabled {
			c.Next()
			return
		}

		if !effectiveAt.IsZero() && now().Before(effectiveAt) {
			c.Next()
			return
		}

		path := normalizeRegionPath(c.Request.URL.Path)
		if isRegionRestrictionExempt(path, restrictedPath, exemptPaths) {
			c.Next()
			return
		}

		country := strings.ToUpper(strings.TrimSpace(c.GetHeader(countryHeader)))
		if _, blocked := blockedCountries[country]; !blocked {
			c.Next()
			return
		}

		c.Header("Cache-Control", "no-store")
		c.Header("X-Region-Restricted", country)
		message := regionRestrictionMessage(effectiveAt)

		if isBrowserNavigation(c.Request) {
			query := url.Values{}
			query.Set("country", country)
			c.Redirect(http.StatusTemporaryRedirect, restrictedPath+"?"+query.Encode())
			c.Abort()
			return
		}

		writeRegionRestrictionError(c, message)
	}
}

func normalizeRegionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func isRegionRestrictionExempt(path, restrictedPath string, exemptPaths []string) bool {
	if path == restrictedPath ||
		strings.HasPrefix(path, "/assets/") ||
		path == "/region-restricted-bg.png" ||
		path == "/logo.svg" ||
		path == "/favicon.ico" ||
		path == "/manifest.webmanifest" {
		return true
	}

	for _, exemptPath := range exemptPaths {
		if path == exemptPath || strings.HasPrefix(path, exemptPath+"/") {
			return true
		}
	}
	return false
}

func isBrowserNavigation(request *http.Request) bool {
	if request == nil || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		return false
	}
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}

func regionRestrictionMessage(effectiveAt time.Time) string {
	if effectiveAt.IsZero() {
		return "中国大陆地区暂不可用。中国香港、中国澳门、中国台湾及其他支持地区不受影响。 Service unavailable in mainland China. Hong Kong, Macao, Taiwan, and other supported regions remain available."
	}

	effectiveAt = effectiveAt.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	chineseTime := effectiveAt.Format("2006年1月2日 15:04")
	englishTime := effectiveAt.Format("January 2, 2006 15:04")
	return fmt.Sprintf(
		"自 %s（北京时间）起，中国大陆地区暂不可用。中国香港、中国澳门、中国台湾及其他支持地区不受影响。 Service unavailable in mainland China from %s (Asia/Shanghai). Hong Kong, Macao, Taiwan, and other supported regions remain available.",
		chineseTime,
		englishTime,
	)
}

func writeRegionRestrictionError(c *gin.Context, message string) {
	path := normalizeRegionPath(c.Request.URL.Path)
	switch {
	case path == "/v1/messages", path == "/v1/messages/count_tokens":
		AnthropicErrorWriter(c, http.StatusForbidden, message)
		c.Abort()
	case strings.HasPrefix(path, "/v1beta/"), strings.HasPrefix(path, "/antigravity/v1beta/"):
		GoogleErrorWriter(c, http.StatusForbidden, message)
		c.Abort()
	case strings.HasPrefix(path, "/v1/"):
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"type":    "permission_error",
				"code":    regionRestrictedCode,
				"message": message,
			},
		})
		c.Abort()
	default:
		AbortWithError(c, http.StatusForbidden, regionRestrictedCode, message)
	}
}
