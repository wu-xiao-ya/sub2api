//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

func TestShouldUseOpenAIResponsesAPICompatiblePlatforms(t *testing.T) {
	for _, platform := range []string{
		PlatformDeepSeek, PlatformKimi, PlatformGLM, PlatformQwen,
		PlatformMiniMax, PlatformMiMo, PlatformHunyuan,
	} {
		t.Run(platform+"_auto_uses_chat_completions", func(t *testing.T) {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey}
			if ShouldUseOpenAIResponsesAPI(account) {
				t.Fatal("auto mode should keep third-party compatible platforms on chat completions")
			}
		})

		t.Run(platform+"_force_responses_is_honored", func(t *testing.T) {
			account := &Account{
				Platform: platform,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{
					openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
				},
			}
			if !ShouldUseOpenAIResponsesAPI(account) {
				t.Fatal("explicit force_responses should override the compatible-platform default")
			}
		})

		t.Run(platform+"_force_chat_completions_is_honored", func(t *testing.T) {
			account := &Account{
				Platform: platform,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{
					openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
				},
			}
			if ShouldUseOpenAIResponsesAPI(account) {
				t.Fatal("force_chat_completions should use chat completions")
			}
		})
	}
}
