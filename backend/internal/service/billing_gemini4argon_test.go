//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Argon is billed through the hardcoded fallback table because the local pricing
// catalog may be unavailable or older than the model. Tier aliases share the
// base rate; the cyber variant keeps its non-promotional Vertex rate.
func TestBillingService_Gemini4ArgonFallbacksAreBillable(t *testing.T) {
	t.Parallel()

	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}

	for _, tc := range []struct {
		model               string
		input, output, read float64
	}{
		{"gemini-4-argon", 2, 10, 0.1},
		{"gemini-4-argon-high", 2, 10, 0.1},
		{"gemini-4-argon-low", 2, 10, 0.1},
		{"gemini-4-argon-medium", 2, 10, 0.1},
		{"models/gemini-4-argon", 2, 10, 0.1},
		{"gemini-4-argon-cyber-permissive", 4, 20, 0.2},
		{"gemini-4-argon-cyber", 4, 20, 0.2},
		{"publishers/google/models/gemini-4-argon-cyber-permissive", 4, 20, 0.2},
	} {
		t.Run(tc.model, func(t *testing.T) {
			cost, err := svc.CalculateCost(tc.model, tokens, 1)
			require.NoError(t, err)
			require.InDelta(t, tc.input, cost.InputCost, 1e-12)
			require.InDelta(t, tc.output, cost.OutputCost, 1e-12)
			require.InDelta(t, tc.read, cost.CacheReadCost, 1e-12)
			require.InDelta(t, tc.input+tc.output+tc.read, cost.TotalCost, 1e-12)
		})
	}
}
