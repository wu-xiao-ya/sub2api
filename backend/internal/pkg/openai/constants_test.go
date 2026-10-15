package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
		require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
		require.Contains(t, DefaultModelIDs(), "gpt-6-sol")
		require.Contains(t, DefaultModelIDs(), "gpt-6-luna")
}
