package repository

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

func TestReqClientRelayConnectBudget(t *testing.T) {
	for _, name := range []string{"standard", "fingerprint", "claude-session"} {
		t.Run(name, func(t *testing.T) {
			closed := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					return
				}
				defer conn.Close()
				defer close(closed)
				_, _ = io.Copy(io.Discard, conn)
			}))
			defer proxy.Close()
			client, err := getSharedReqClient(reqClientOptions{ProxyURL: proxy.URL, Timeout: 3 * time.Second, Impersonate: name == "fingerprint"})
			if name == "claude-session" {
				client, err = createReqClient(proxy.URL)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer client.GetTransport().CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			u, _ := url.Parse(proxy.URL)
			_, err = client.R().SetContext(proxyutil.WithRelayConnectHost(ctx, u.Host)).Get("https://example.test")
			var connectErr *proxyutil.ConnectError
			if !errors.As(err, &connectErr) || ctx.Err() != nil {
				t.Fatalf("no safe fallback budget: %v, parent=%v", err, ctx.Err())
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("CONNECT socket leaked")
			}
		})
	}
}
