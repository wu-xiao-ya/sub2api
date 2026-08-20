package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

const (
	regionRestrictedCode    = "REGION_RESTRICTED"
	regionRestrictedMessage = "This service is not available in your region."
)

// RegionRestriction uses a trusted CDN country header to block configured
// regions. A missing country header fails open for local checks and development.
func RegionRestriction(cfg config.RegionRestrictionConfig) gin.HandlerFunc {
	return newRegionRestriction(cfg, time.Now)
}

func newRegionRestriction(cfg config.RegionRestrictionConfig, now func() time.Time) gin.HandlerFunc {
	if !cfg.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

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

		if isBrowserNavigation(c.Request) {
			query := url.Values{}
			query.Set("country", country)
			c.Redirect(http.StatusTemporaryRedirect, restrictedPath+"?"+query.Encode())
			c.Abort()
			return
		}

		writeRegionRestrictionError(c)
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

func writeRegionRestrictionError(c *gin.Context) {
	path := normalizeRegionPath(c.Request.URL.Path)
	switch {
	case path == "/v1/messages", path == "/v1/messages/count_tokens":
		AnthropicErrorWriter(c, http.StatusForbidden, regionRestrictedMessage)
	case strings.HasPrefix(path, "/v1beta/"), strings.HasPrefix(path, "/antigravity/v1beta/"):
		GoogleErrorWriter(c, http.StatusForbidden, regionRestrictedMessage)
	case strings.HasPrefix(path, "/v1/"):
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"type":    "permission_error",
				"code":    regionRestrictedCode,
				"message": regionRestrictedMessage,
			},
		})
		c.Abort()
	default:
		AbortWithError(c, http.StatusForbidden, regionRestrictedCode, regionRestrictedMessage)
	}
}
