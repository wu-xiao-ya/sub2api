package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/gin-gonic/gin"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type providerTestEncryptor struct{}

func (providerTestEncryptor) Encrypt(s string) (string, error) { return s, nil }
func (providerTestEncryptor) Decrypt(s string) (string, error) { return s, nil }

type providerConfigAPI struct {
	pluginv1.TransportPluginClient
	applied int
}

func (a *providerConfigAPI) ValidateConfig(_ context.Context, r *pluginv1.ValidateConfigRequest, _ ...grpc.CallOption) (*pluginv1.ValidateConfigResponse, error) {
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: r.ConfigJson}, nil
}
func (a *providerConfigAPI) ApplyConfig(context.Context, *pluginv1.ApplyConfigRequest, ...grpc.CallOption) (*pluginv1.ApplyConfigResponse, error) {
	a.applied++
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}

func TestPluginProviderConfigReconcileKeepsInflightStreams(t *testing.T) {
	api := &providerConfigAPI{}
	client := hcplugin.NewClient(&hcplugin.ClientConfig{})
	defer client.Kill()
	runtime := &pluginRuntime{client: client, api: api, installation: &PluginInstallation{ID: 7, BinarySHA256: "same", ConfigEncrypted: `{}`}}
	runtime.inFlight.Store(1)
	manager := &PluginManager{encryptor: providerTestEncryptor{}, providerRuntimes: map[int64]*pluginRuntime{7: runtime}}
	next := &PluginInstallation{ID: 7, State: PluginStateEnabled, BinarySHA256: "same", ConfigEncrypted: `{"revision":2}`, Manifest: PluginManifest{Capabilities: []PluginCapability{{ID: PluginCapabilityChatCompletions}}}, Bindings: []PluginBinding{{Enabled: true, Capability: PluginCapabilityChatCompletions}}}
	require.NoError(t, manager.reconcileProviders(context.Background(), []*PluginInstallation{next}))
	require.Same(t, runtime, manager.providerRuntimes[7])
	require.Equal(t, int64(1), runtime.inFlight.Load())
	require.False(t, runtime.draining.Load())
	require.Equal(t, 1, api.applied)
	require.Equal(t, next.ConfigEncrypted, runtime.installation.ConfigEncrypted)
}

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

func TestPluginProviderChatCompletionsUsesPluginTransportWithoutAPIKey(t *testing.T) {
	upstream := &pluginRoutingHTTPUpstream{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 284, Type: AccountTypePlugin, Platform: PlatformDeepseek}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	target, err := gateway.rawChatCompletionsURL(account)
	require.NoError(t, err)
	require.Equal(t, "https://plugin.invalid/v1/chat/completions", target)
	_, err = gateway.ForwardAsChatCompletions(c.Request.Context(), c, account,
		[]byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}`), "", "")
	var transportErr *PluginTransportError
	require.ErrorAs(t, err, &transportErr)
	require.Equal(t, "plugin_unavailable", transportErr.Code)
	require.True(t, transportErr.NoReplay)
	require.Zero(t, upstream.doCalls)
}

func TestPluginProviderRejectsStatefulFeaturesBeforeConversion(t *testing.T) {
	a := &Account{Type: AccountTypePlugin, Platform: PlatformDeepseek}
	for _, body := range []string{`{"background":true}`, `{"store":true}`, `{"previous_response_id":"resp_fixture"}`} {
		require.Error(t, validateProviderIngress(a, []byte(body)))
		require.NoError(t, validateProviderIngress(&Account{Type: AccountTypeAPIKey}, []byte(body)))
	}
	require.NoError(t, validateProviderIngress(a, []byte(`{"store":false,"stream":true}`)))
}
