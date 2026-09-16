package repository

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamLatencyRealTLSAndProxy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated Linux child uses SSL_CERT_FILE without changing host trust")
	}
	if target := os.Getenv("PERFORMANCE_TLS_CHILD_URL"); target != "" {
		parsed, err := url.Parse(target)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "https", parsed.Scheme)
		client := NewHTTPUpstream(nil)
		for _, proxy := range []string{"", os.Getenv("PERFORMANCE_TLS_CHILD_PROXY")} {
			for _, header := range []string{"Authorization", "x-api-key"} {
				ctx := service.WithRequestLatency(t.Context())
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"/v1/responses", strings.NewReader("{}"))
				require.NoError(t, err)
				value := "test-only-key"
				if header == "Authorization" {
					value = "Bearer test-only-token"
				}
				req.Header.Set(header, value)
				resp, err := client.DoWithTLS(req, proxy, 7, 1, &tlsfingerprint.Profile{Name: "isolated-node-fingerprint"})
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.Contains(t, string(body), "hello")
				stages := service.FinalRequestLatencySnapshot(ctx)
				require.Equal(t, 2, stages.Version)
				require.Equal(t, 1, stages.AttemptCount)
				require.NotNil(t, stages.FirstResponseMs)
				require.NotNil(t, stages.FirstEventMs)
				require.NotNil(t, stages.FirstOutputMs)
				require.NotNil(t, stages.FirstCharacterMs)
				require.GreaterOrEqual(t, *stages.TotalDurationMs, *stages.FirstCharacterMs)
			}
		}
		return
	}
	var mu sync.Mutex
	var cipherCounts []int
	var authHeaders []string
	var tunnels int
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("x-api-key")
		}
		mu.Lock()
		authHeaders = append(authHeaders, auth)
		mu.Unlock()
		if auth != "Bearer test-only-token" && auth != "test-only-key" {
			http.Error(w, "missing fixture credential", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"hello\"}\n\n")
	}))
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		cipherCounts = append(cipherCounts, len(hello.CipherSuites))
		mu.Unlock()
		return nil, nil
	}}
	up.StartTLS()
	defer up.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != up.Listener.Addr().String() {
			http.Error(w, "unexpected tunnel target", http.StatusForbidden)
			return
		}
		remote, err := net.DialTimeout("tcp", up.Listener.Addr().String(), 5*time.Second)
		if err != nil {
			http.Error(w, "fixture target unavailable", 502)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			return
		}
		defer conn.Close()
		defer remote.Close()
		mu.Lock()
		tunnels++
		mu.Unlock()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		go func() { _, _ = io.Copy(remote, rw); _ = remote.Close() }()
		_, _ = io.Copy(conn, remote)
	}))
	defer proxy.Close()
	cert := filepath.Join(t.TempDir(), "fixture-ca.pem")
	require.NoError(t, os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: up.Certificate().Raw}), 0600))
	// crypto/x509 caches system roots. A fresh test process confines the temporary
	// CA to this test and never disables verification or modifies the host store.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPUpstreamLatencyRealTLSAndProxy$", "-test.v")
	child.Env = append(os.Environ(), "PERFORMANCE_TLS_CHILD_URL="+up.URL, "PERFORMANCE_TLS_CHILD_PROXY="+proxy.URL, "SSL_CERT_FILE="+cert)
	output, err := child.CombinedOutput()
	require.NoError(t, err, "%s", output)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"Bearer test-only-token", "test-only-key", "Bearer test-only-token", "test-only-key"}, authHeaders)
	require.GreaterOrEqual(t, tunnels, 1)
	require.GreaterOrEqual(t, len(cipherCounts), 2, "direct and proxied TLS must both handshake")
	for _, count := range cipherCounts {
		require.Equal(t, 17, count, "configured Node fingerprint must reach the upstream")
	}
}
