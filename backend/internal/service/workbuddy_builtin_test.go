package service

import (
	"context"
	"os"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestBuiltinWorkBuddyStartsFromHostExecutable(t *testing.T) {
	binary := os.Getenv("SUB2API_BUILTIN_WORKBUDDY_TEST_BINARY")
	if binary == "" {
		t.Skip("requires a built Sub2API executable")
	}
	original := &PluginInstallation{
		PluginKey: workBuddyBuiltinID, Version: "0.1.1",
		Manifest: PluginManifest{ID: workBuddyBuiltinID, Capabilities: []PluginCapability{{ID: PluginCapabilityChatCompletions}}},
	}
	local, err := localBuiltinWorkBuddy(original)
	require.NoError(t, err)
	require.Equal(t, "0.1.1", original.Version)
	require.False(t, original.Builtin)
	local.BinaryPath = binary
	runtime, err := startPluginRuntime(context.Background(), local, 15*time.Second, t.TempDir(), nil)
	require.NoError(t, err)
	defer runtime.kill()
	require.NoError(t, runtime.validateAndApplyConfig(context.Background(), []byte(`{}`)))
	result, err := runtime.api.Authorize(context.Background(), &pluginv1.AuthorizeRequest{
		Action: "import", ConfigJson: []byte(`{}`),
		SessionId: `{"domain":"codebuddy.ai","access_token":"redaction-fixture"}`,
	})
	require.NoError(t, err)
	require.False(t, result.ConfigChanged)
	require.Contains(t, string(result.PublicJson), "invalid_credentials_or_region")
	require.NotContains(t, string(result.PublicJson), "redaction-fixture")
}
