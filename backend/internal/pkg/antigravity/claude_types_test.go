package antigravity

import "testing"

func TestDefaultModels_ContainsNewAndLegacyImageModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byID := make(map[string]ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	requiredIDs := []string{
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-opus-4-6-thinking",
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image", // legacy compatibility
		"gemini-4-argon-high",
		"gemini-4-argon-low",
		"gemini-4-argon-medium",
	}

	for _, id := range requiredIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected model %q to be exposed in DefaultModels", id)
		}
	}
}

func TestIsGeminiReasoningModel_Gemini4Argon(t *testing.T) {
	t.Parallel()

	// Argon tier suffixes may arrive without a trailing tier (base ID) from
	// native Gemini clients, so the prefix check must cover both shapes.
	for _, id := range []string{"gemini-4-argon", "gemini-4-argon-high", "gemini-4-argon-low", "gemini-4-argon-medium"} {
		if !IsGeminiReasoningModel(id) {
			t.Fatalf("expected %q to be treated as a reasoning model", id)
		}
	}
	if IsGeminiReasoningModel("gemini-2.5-flash") {
		t.Fatalf("did not expect gemini-2.5-flash to be treated as a reasoning model")
	}
}
