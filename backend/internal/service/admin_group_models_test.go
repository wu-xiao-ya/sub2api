package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultOpenAICompatibleModelIDs(t *testing.T) {
	t.Run("deepseek", func(t *testing.T) {
		require.Equal(t, []string{
			"deepseek-v4-pro",
			"deepseek-v4-flash",
			"deepseek-chat",
			"deepseek-reasoner",
		}, defaultOpenAICompatibleModelIDs(PlatformDeepSeek))
	})

	t.Run("kimi", func(t *testing.T) {
		require.Equal(t, []string{
			"kimi-k3",
			"kimi-k2.6",
			"kimi-for-coding",
			"kimi-k2.5",
			"kimi-k2-thinking",
			"kimi-k2",
			"moonshot-v1-8k",
			"moonshot-v1-32k",
			"moonshot-v1-128k",
		}, defaultOpenAICompatibleModelIDs(PlatformKimi))
	})

	t.Run("glm", func(t *testing.T) {
		require.Equal(t, []string{
			"glm-5.3-flash",
			"glm-5.3",
			"glm-5.2",
			"glm-5.1",
			"glm-5",
			"glm-5-turbo",
			"glm-4.7",
			"glm-4.7-flash",
			"glm-4.7-flashx",
			"glm-4.6",
			"glm-4.5",
		}, defaultOpenAICompatibleModelIDs(PlatformGLM))
	})

	t.Run("new domestic aggregate platforms", func(t *testing.T) {
		require.Equal(t, []string{"qwen3.8-max", "qwen3.7-max", "qwen3.7-plus"}, defaultOpenAICompatibleModelIDs(PlatformQwen))
		require.Equal(t, []string{"minimax-m3", "minimax-m2.7", "minimax-m2.7-highspeed"}, defaultOpenAICompatibleModelIDs(PlatformMiniMax))
		require.Equal(t, []string{"mimo-v2.5-pro", "mimo-v2.5"}, defaultOpenAICompatibleModelIDs(PlatformMiMo))
		require.Equal(t, []string{"hy3", "hunyuan-hy3"}, defaultOpenAICompatibleModelIDs(PlatformHunyuan))
	})
}

func TestDefaultModelsListCandidateIDs_OpenAICompatiblePlatforms(t *testing.T) {
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformDeepSeek), defaultModelsListCandidateIDs(PlatformDeepSeek))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformKimi), defaultModelsListCandidateIDs(PlatformKimi))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformGLM), defaultModelsListCandidateIDs(PlatformGLM))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformQwen), defaultModelsListCandidateIDs(PlatformQwen))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformMiniMax), defaultModelsListCandidateIDs(PlatformMiniMax))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformMiMo), defaultModelsListCandidateIDs(PlatformMiMo))
	require.Equal(t, defaultOpenAICompatibleModelIDs(PlatformHunyuan), defaultModelsListCandidateIDs(PlatformHunyuan))
}
