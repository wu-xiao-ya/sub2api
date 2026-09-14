package repository

import (
	"compress/gzip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPUpstreamLatencyKeepsAuthenticationProxyAndDecodedBody(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"openai", "/v1/responses", "event: response.output_text.delta\ndata: {\"delta\":\"hello\"}\n\n"},
		{"compatible", "/v1/chat/completions", "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"},
		{"anthropic", "/v1/messages", "event: content_block_delta\ndata: {\"delta\":{\"text\":\"hello\"}}\n\n"},
		{"gemini", "/v1beta/models/gemini:streamGenerateContent", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "Bearer test-only-credential", r.Header.Get("Authorization"))
				require.Equal(t, tc.path, r.URL.Path)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Content-Encoding", "gzip")
				z := gzip.NewWriter(w)
				_, err := io.WriteString(z, tc.body)
				require.NoError(t, err)
				require.NoError(t, z.Close())
			}))
			defer proxy.Close()
			ctx := service.WithRequestLatency(t.Context())
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://upstream.invalid"+tc.path, strings.NewReader("{}"))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-only-credential")
			req.Header.Set("Accept-Encoding", "gzip")
			upstream := NewHTTPUpstream(nil)
			resp, err := upstream.DoWithTLS(req, proxy.URL, 1, 1, &tlsfingerprint.Profile{Name: "test-plain-http"})
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.body, string(body))
			require.Equal(t, int64(1), calls.Load())
			b := service.FinalRequestLatencySnapshot(ctx)
			require.NotNil(t, b)
			require.Equal(t, 2, b.Version)
			require.Equal(t, 1, b.AttemptCount)
			require.NotNil(t, b.FirstResponseMs)
			require.NotNil(t, b.FirstEventMs)
			require.NotNil(t, b.FirstOutputMs)
			require.NotNil(t, b.FirstCharacterMs)
			require.GreaterOrEqual(t, *b.FirstEventMs, *b.FirstResponseMs)
			require.GreaterOrEqual(t, *b.TotalDurationMs, *b.FirstCharacterMs)
		})
	}
}
