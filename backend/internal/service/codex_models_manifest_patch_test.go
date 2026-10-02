package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// gpt-6.1-sol shipped while the upstream Codex models manifest generator still
// classified it as unknown, emitting a single "none" reasoning level. Codex
// clients trust that manifest for their effort picker, so users reported the
// max reasoning tier as unavailable. The gateway now patches the entry.

const codexManifestPatchFixture = `{"models":[` +
	`{"slug":"gpt-6-sol","display_name":"GPT-6 Sol","supported_reasoning_levels":[` +
	`{"effort":"low","description":"Fast responses with lighter reasoning"},` +
	`{"effort":"medium","description":"Balanced reasoning for most coding tasks"}],"default_reasoning_level":"medium"},` +
	`{"slug":"gpt-6.1-sol","display_name":"GPT-6.1 Sol","supported_reasoning_levels":[` +
	`{"effort":"none","description":"Use the model's default behavior without configurable reasoning"}],"default_reasoning_level":"none"},` +
	`{"slug":"gpt-6-astra","display_name":"GPT-6 Astra","supported_reasoning_levels":[` +
	`{"effort":"low","description":"Fast responses with lighter reasoning"}],"default_reasoning_level":"low"}` +
	`],"extra":"keep"}`

func extractPatchedEntry(t *testing.T, body []byte, slug string) map[string]any {
	t.Helper()
	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	for _, entry := range envelope.Models {
		if entry["slug"] == slug {
			return entry
		}
	}
	t.Fatalf("entry %s missing from patched manifest", slug)
	return nil
}

func TestPatchCodexModelsManifestReasoningLevels_FillsUnknownModel(t *testing.T) {
	patched := patchCodexModelsManifestReasoningLevels([]byte(codexManifestPatchFixture))

	entry := extractPatchedEntry(t, patched, "gpt-6.1-sol")
	levels, ok := entry["supported_reasoning_levels"].([]any)
	require.True(t, ok, "levels must become an array")
	require.Len(t, levels, 5)
	efforts := make([]string, 0, len(levels))
	for _, level := range levels {
		efforts = append(efforts, level.(map[string]any)["effort"].(string))
	}
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, efforts)
	require.Equal(t, "medium", entry["default_reasoning_level"])
	// The rest of the entry must survive the patch.
	require.Equal(t, "GPT-6.1 Sol", entry["display_name"])

	// Sibling entries stay byte-equivalent in meaning: untouched slugs keep
	// their own levels and defaults.
	sol := extractPatchedEntry(t, patched, "gpt-6-sol")
	require.Len(t, sol["supported_reasoning_levels"].([]any), 2)
	require.Equal(t, "medium", sol["default_reasoning_level"])
	astra := extractPatchedEntry(t, patched, "gpt-6-astra")
	require.Len(t, astra["supported_reasoning_levels"].([]any), 1)

	require.Contains(t, string(patched), `"extra":"keep"`)
}

func TestPatchCodexModelsManifestReasoningLevels_KeepsRealLevels(t *testing.T) {
	// Once the upstream manifest advertises real tiers for gpt-6.1-sol the
	// patch must not fire (their levels win).
	body := `{"models":[{"slug":"gpt-6.1-sol","supported_reasoning_levels":[` +
		`{"effort":"low","description":"Fast"}],"default_reasoning_level":"low"}]}`
	patched := patchCodexModelsManifestReasoningLevels([]byte(body))
	require.Equal(t, body, string(patched))
}

func TestPatchCodexModelsManifestReasoningLevels_LeavesUnknownShapesAlone(t *testing.T) {
	require.Equal(t, `{"data":[]}`, string(patchCodexModelsManifestReasoningLevels([]byte(`{"data":[]}`))))
	require.Equal(t, `not-json`, string(patchCodexModelsManifestReasoningLevels([]byte(`not-json`))))
}

func TestSupportsOpenAIMaxReasoningEffort_SixSeriesSol(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6.1-sol-high", "gpt-6-sol", "gpt-6-sol-high", "gpt-6-astra", "gpt-5.6-sol"} {
		require.True(t, supportsOpenAIMaxReasoningEffort(model), model)
	}
	for _, model := range []string{"gpt-5.5", "claude-opus-5-5", "gpt-5.4"} {
		require.False(t, supportsOpenAIMaxReasoningEffort(model), model)
	}
}
