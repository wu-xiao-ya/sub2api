package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"time"
)

func TestAccountDefaultProxyURL(t *testing.T) {
	if AccountDefaultProxyURL(nil) != "" {
		t.Fatal("nil account")
	}
	id := int64(9)
	account := &Account{ProxyID: &id, Proxy: &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}}
	got := AccountDefaultProxyURL(account)
	if got == "" {
		t.Fatal("expected proxy url")
	}
}

func TestResolveAccountProxyURLWithoutRelay(t *testing.T) {
	id := int64(3)
	account := &Account{ID: 1, ProxyID: &id, Proxy: &Proxy{Protocol: "http", Host: "10.0.0.1", Port: 3128}}
	if ResolveAccountProxyURL(account) != AccountDefaultProxyURL(account) {
		t.Fatal("unchecked account should use own proxy")
	}
}

func TestResolveAccountProxyURLWhenRelayDownUsesFallback(t *testing.T) {
	id := int64(3)
	account := &Account{
		ID:            7,
		UseRelayRoute: true,
		ProxyID:       &id,
		Proxy:         &Proxy{Protocol: "http", Host: "10.0.0.1", Port: 3128},
	}
	svc := &AccountOutboundService{}
	svc.relaySnap = cachedRelaySnapshot{url: "http://relay.example:38480", host: "relay.example:38480", ttl: time.Minute, expires: time.Now().Add(time.Minute)}
	svc.MarkDown(context.Background(), AccountOutbound{ProxyURL: svc.relaySnap.url})
	SetProcessAccountOutbound(svc)
	t.Cleanup(func() { SetProcessAccountOutbound(nil) })
	if got, want := ResolveAccountProxyURL(account), AccountDefaultProxyURL(account); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsRelayConnectError(t *testing.T) {
	if !IsRelayConnectError(errors.New("proxyconnect tcp: dial tcp 43.133.208.166:38480: i/o timeout"), "43.133.208.166:38480") {
		t.Fatal("proxyconnect")
	}
	if IsRelayConnectError(errors.New("tls: handshake failure"), "43.133.208.166:38480") {
		t.Fatal("origin tls must not be a relay failure")
	}
	if !IsRelayConnectError(errors.New("dial tcp 43.133.208.166:38480: connect: connection refused"), "43.133.208.166:38480") {
		t.Fatal("connection refused to relay host")
	}
	if IsRelayConnectError(errors.New("http 403 from upstream"), "43.133.208.166:38480") {
		t.Fatal("origin http must not be a relay failure")
	}
}

func TestRetryRelayHTTPFallsBackOnce(t *testing.T) {
	account := &Account{ID: 42, UseRelayRoute: true}
	req, err := http.NewRequest(http.MethodGet, "https://www.f-api.site/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	req = testRelayRequest(req, account.ID, "http://own-proxy:3128")
	resp, retryErr, retried := RetryRelayHTTP(context.Background(), account.ID, "http://relay.example:38480", req, errors.New("proxyconnect tcp: i/o timeout"), func(r *http.Request, proxyURL string) (*http.Response, error) {
		calls++
		if proxyURL != "http://own-proxy:3128" {
			t.Fatalf("fallback proxy %q", proxyURL)
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	if !retried || retryErr != nil || resp == nil || resp.StatusCode != 200 || calls != 1 {
		t.Fatalf("retried=%v err=%v calls=%d resp=%v", retried, retryErr, calls, resp)
	}
}

func TestRetryRelayHTTPDoesNotRetryOriginTLS(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://www.f-api.site/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = testRelayRequest(req, 9, "")
	_, retryErr, retried := RetryRelayHTTP(context.Background(), 9, "http://relay.example:38480", req, errors.New("tls: handshake failure"), func(*http.Request, string) (*http.Response, error) {
		t.Fatal("must not retry origin tls")
		return nil, nil
	})
	if retried || retryErr == nil {
		t.Fatalf("retried=%v err=%v", retried, retryErr)
	}
}

func TestRetryRelayHTTPDoesNotRetryCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, retried := RetryRelayHTTP(ctx, 9, "http://relay.example:38480", req,
		errors.New("proxyconnect tcp: i/o timeout"), func(*http.Request, string) (*http.Response, error) {
			t.Fatal("cancelled request must not be retried")
			return nil, nil
		})
	if retried {
		t.Fatal("cancelled request was retried")
	}
}

type responseAndErrorUpstream struct{}

func (responseAndErrorUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody},
		errors.New("proxyconnect tcp: i/o timeout")
}

func (s responseAndErrorUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxy, id, concurrency)
}

func TestWrapRelayHTTPPreservesResponseOnError(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	upstream := WrapRelayHTTPUpstream(responseAndErrorUpstream{})
	for _, withTLS := range []bool{false, true} {
		var resp *http.Response
		var err error
		if withTLS {
			resp, err = upstream.DoWithTLS(req, "", 1, 1, nil)
		} else {
			resp, err = upstream.Do(req, "", 1, 1)
		}
		if resp == nil || resp.StatusCode != http.StatusBadGateway || err == nil {
			t.Fatalf("withTLS=%v resp=%v err=%v", withTLS, resp, err)
		}
	}
}

type stubRelayHTTP struct {
	calls []string
	fail  map[string]error
}

func (s *stubRelayHTTP) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	s.calls = append(s.calls, "do:"+proxyURL)
	if err := s.fail[proxyURL]; err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

func (s *stubRelayHTTP) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestWrapRelayHTTPUpstreamRetriesConnect(t *testing.T) {
	inner := &stubRelayHTTP{fail: map[string]error{
		"http://relay.example:38480": errors.New("proxyconnect tcp: i/o timeout"),
	}}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = testRelayRequest(req, 11, "http://own-proxy:3128")
	resp, retryErr := WrapRelayHTTPUpstream(inner).Do(req, "http://relay.example:38480", 11, 1)
	if retryErr != nil || resp == nil || resp.StatusCode != 200 {
		t.Fatalf("err=%v resp=%v", retryErr, resp)
	}
	if len(inner.calls) != 2 || inner.calls[0] != "do:http://relay.example:38480" || inner.calls[1] != "do:http://own-proxy:3128" {
		t.Fatalf("calls=%v", inner.calls)
	}
}

func testRelayRequest(req *http.Request, id int64, fallback string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), accountOutboundContextKey{},
		requestAccountOutbound{accountID: id, outbound: AccountOutbound{
			ProxyURL: "http://relay.example:38480", ViaRelay: true,
			FallbackProxyURL: fallback, RelayHost: "relay.example:38480",
		}}))
}

func TestAccountOutboundRequestIsolation(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	first := testRelayRequest(req, 42, "http://first:3128")
	second := testRelayRequest(req, 42, "http://second:3128")
	for _, test := range []struct {
		req      *http.Request
		fallback string
	}{
		{first, "http://first:3128"}, {second, "http://second:3128"},
	} {
		route, ok := requestOutbound(test.req, 42)
		if !ok || route.FallbackProxyURL != test.fallback {
			t.Fatalf("request route overwritten: %+v", route)
		}
		if _, ok := requestOutbound(test.req, 43); ok {
			t.Fatal("route must not cross account boundaries")
		}
	}
	if _, ok := requestOutbound(req, 42); ok {
		t.Fatal("original request must remain unmodified")
	}
}

func TestAccountOutboundBindingPreservesOverrideAndDisabledAccounts(t *testing.T) {
	svc := installTestRelay(t)
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	account := &Account{ID: 19, UseRelayRoute: true}
	bound := WithAccountOutbound(req, account, "")
	route, ok := requestOutbound(bound, 19)
	if !ok || !route.ViaRelay || route.ProxyURL != svc.relaySnap.url {
		t.Fatalf("missing relay snapshot: %+v", route)
	}
	overridden := WithAccountOutbound(req, account, "http://explicit:8080")
	override, _ := requestOutbound(overridden, 19)
	if override.ViaRelay || override.ProxyURL != "http://explicit:8080" {
		t.Fatalf("explicit override lost: %+v", override)
	}
	account.UseRelayRoute = false
	disabled := WithAccountOutbound(req, account, "")
	direct, _ := requestOutbound(disabled, 19)
	if direct.ViaRelay || direct.ProxyURL != "" {
		t.Fatalf("unchecked account changed route: %+v", direct)
	}
	old, _ := requestOutbound(bound, 19)
	if !old.ViaRelay {
		t.Fatal("later account changes modified an in-flight route")
	}
}

func TestRetryRelayNonReplayableBodyStillMarksOutage(t *testing.T) {
	svc := installTestRelay(t)
	req, _ := http.NewRequest(http.MethodPost, "https://example.test",
		io.NopCloser(strings.NewReader("payload")))
	req = testRelayRequest(req, 9, "")
	_, _, retried := RetryRelayHTTP(req.Context(), 9, svc.relaySnap.url, req,
		errors.New("proxyconnect tcp: i/o timeout"), func(*http.Request, string) (*http.Response, error) {
			t.Fatal("non-replayable body must not be retried")
			return nil, nil
		})
	if retried || !svc.isDown(context.Background(), svc.relaySnap.url) {
		t.Fatal("relay outage must be recorded even when replay is impossible")
	}
}
