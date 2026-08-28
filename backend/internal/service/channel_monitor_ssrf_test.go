package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoopbackDialContextAllowsOnlyLoopback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	address := net.JoinHostPort("127.0.0.1", port)

	conn, err := loopbackDialContext(context.Background(), "tcp", address)
	require.NoError(t, err)
	_ = conn.Close()

	_, err = safeDialContext(context.Background(), "tcp", address)
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked by SSRF policy")

	_, err = loopbackDialContext(context.Background(), "tcp", net.JoinHostPort("10.0.0.1", strconv.Itoa(80)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-loopback address blocked")
}

func TestInternalMonitorClientCanCallLoopbackGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "1"}},
			},
		})
	}))
	t.Cleanup(server.Close)

	endpoint := server.URL
	result := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-monitor", "gpt-test", &CheckOptions{
		LowCost:    true,
		httpClient: monitorInternalGatewayHTTPClient,
	})
	require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
}
