package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginProviderManifestScopeAndLegacyCompatibility(t *testing.T) {
	m := testPluginManifest(nil)
	require.NoError(t, m.Validate())
	provider := PluginCapability{ID: PluginCapabilityChatCompletions, Platform: PlatformDeepseek, AccountType: AccountTypePlugin}
	m.Capabilities = []PluginCapability{provider}
	require.NoError(t, m.Validate())
	// A provider never gets the global account/credential directory.
	require.True(t, pluginAccountScopeFromManifest(m).Empty())
	m.Capabilities[0].Platform = PlatformOpenAI
	require.Error(t, m.Validate())
	m.Capabilities = []PluginCapability{provider, {ID: PluginCapabilityOpenAIOAuthOutbound, Platform: PlatformOpenAI, AccountType: AccountTypeOAuth}}
	require.Error(t, m.Validate())
}
func TestPluginProviderCannotFallBackToHTTPWithoutManagerOrBinding(t *testing.T) {
	upstream := &pluginRoutingHTTPUpstream{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	req, e := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://plugin.invalid/v1/chat/completions", nil)
	require.NoError(t, e)
	account := &Account{ID: 7, Type: AccountTypePlugin, Platform: PlatformDeepseek}
	response, e := gateway.doOpenAIUpstream(req, "", account)
	require.Error(t, e)
	require.Nil(t, response)
	require.Zero(t, upstream.doCalls)
	gateway.pluginManager = &PluginManager{}
	response, e = gateway.doOpenAIUpstream(req, "", account)
	require.Error(t, e)
	require.Nil(t, response)
	require.Zero(t, upstream.doCalls)
}
func TestPluginProviderForcesChatConversionAcrossIngressProtocols(t *testing.T) {
	a := &Account{Type: AccountTypePlugin, Platform: PlatformDeepseek}
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(a))
	for _, inbound := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		require.Equal(t, APIProtocolChatCompletions, resolveUpstreamProtocol(a, inbound, "deepseek-v4-pro", nil))
	}
	key, target, e := (&OpenAIGatewayService{}).resolveCCFallbackTarget(a)
	require.NoError(t, e)
	require.Empty(t, key)
	require.Equal(t, "https://plugin.invalid/v1/chat/completions", target)
}
