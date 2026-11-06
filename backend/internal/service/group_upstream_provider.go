package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// UpstreamProvider describes the brand; Platform remains the routing authority.
func NormalizeGroupUpstreamProvider(platform, provider string) (string, error) {
	switch provider {
	case "":
		return "", nil
	case "qwen", "hunyuan", "mimo":
		if platform == PlatformOpenAI {
			return provider, nil
		}
	}
	return "", infraerrors.BadRequest("INVALID_UPSTREAM_PROVIDER", "upstream_provider must be empty or qwen/hunyuan/mimo on an openai group")
}
