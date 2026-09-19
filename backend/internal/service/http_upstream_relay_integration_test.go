package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type relayTransportTestClient struct{}

func bindTestOutbound(req *http.Request, id int64, route AccountOutbound) *http.Request {
	if route.ViaRelay {
		req = req.WithContext(proxyutil.WithRelayConnectHost(req.Context(), route.RelayHost))
	}
	return req.WithContext(context.WithValue(req.Context(), accountOutboundContextKey{},
		requestAccountOutbound{accountID: id, outbound: route}))
}

func (relayTransportTestClient) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Test origin uses a self-signed certificate.
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
		proxyutil.ConfigureRelayConnectBudget(transport, u)
	}
	defer transport.CloseIdleConnections()
	return (&http.Client{Transport: transport}).Do(req)
}

func TestRelayRealStalledConnectFallsBackToOwnProxy(t *testing.T) {
	closed := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(closed)
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer relay.Close()
	var fallbackCalls atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || string(body) != `{"model":"test"}` ||
			r.Header.Get("Authorization") != "Bearer test" {
			t.Error("fallback changed request")
		}
		_, _ = w.Write([]byte("own proxy"))
	}))
	defer fallback.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.test/generate", bytes.NewBufferString(`{"model":"test"}`))
	req.Header.Set("Authorization", "Bearer test")
	route := AccountOutbound{ProxyURL: relay.URL, ViaRelay: true,
		RelayHost: relay.Listener.Addr().String(), FallbackProxyURL: fallback.URL}
	req = bindTestOutbound(req, 9, route)
	resp, err := WrapRelayHTTPUpstream(relayTransportTestClient{}).Do(req, relay.URL, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "own proxy" || fallbackCalls.Load() != 1 {
		t.Fatalf("fallback body=%q calls=%d err=%v", body, fallbackCalls.Load(), err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("stalled relay socket leaked")
	}
}

func (c relayTransportTestClient) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return c.Do(req, proxy, id, concurrency)
}

func TestRelayRealConnectAuthenticationFailureReplaysBody(t *testing.T) {
	var originCalls atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"model":"test"}` || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("fallback lost body or credentials")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	var connectCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectCalls.Add(1)
		if r.Method != http.MethodConnect {
			t.Errorf("expected CONNECT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	req, _ := http.NewRequest(http.MethodPost, origin.URL, bytes.NewBufferString(`{"model":"test"}`))
	req.Header.Set("Authorization", "Bearer test")
	route := AccountOutbound{ProxyURL: proxy.URL, ViaRelay: true, RelayHost: proxy.Listener.Addr().String()}
	req = bindTestOutbound(req, 9, route)
	resp, err := WrapRelayHTTPUpstream(relayTransportTestClient{}).Do(req, proxy.URL, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || originCalls.Load() != 1 || connectCalls.Load() != 1 {
		t.Fatalf("status=%d origin=%d proxy=%d", resp.StatusCode, originCalls.Load(), connectCalls.Load())
	}
}

func TestRelayConcurrentFallbackKeepsEachRequestRoute(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fallback := "http://first:3128"
			if i%2 == 0 {
				fallback = "http://second:3128"
			}
			req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
			req = testRelayRequest(req, 42, fallback)
			_, err, retried := RetryRelayHTTP(req.Context(), 42, "http://relay.example:38480",
				req, errors.New("proxyconnect tcp: i/o timeout"),
				func(_ *http.Request, actual string) (*http.Response, error) {
					if actual != fallback {
						t.Errorf("request %d used %q, wanted %q", i, actual, fallback)
					}
					return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
				})
			if err != nil || !retried {
				t.Errorf("request %d: retry=%v err=%v", i, retried, err)
			}
		}(i)
	}
	wg.Wait()
}
