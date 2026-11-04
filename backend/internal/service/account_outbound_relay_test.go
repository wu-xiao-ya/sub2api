package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type relayTestUpstream struct {
	calls      []string
	relayError error
	body       string
}

type relayTestWSDialer struct {
	calls       []string
	relayError  error
	relayStatus int
}

func (d *relayTestWSDialer) Dial(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
	d.calls = append(d.calls, proxyURL)
	if strings.Contains(proxyURL, "relay.example") {
		if d.relayError == nil {
			d.relayError = errors.New("proxyconnect tcp: dial tcp relay.example:38480: i/o timeout")
		}
		return nil, d.relayStatus, http.Header{"X-Test": []string{"relay"}}, d.relayError
	}
	return &openAIWSFakeConn{}, http.StatusSwitchingProtocols, http.Header{"X-Test": []string{"fallback"}}, nil
}

func (u *relayTestUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.calls = append(u.calls, proxyURL)
	if strings.Contains(proxyURL, "relay.example") {
		return nil, u.relayError
	}
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.body = string(body)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func (u *relayTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func installRelayTestService(t *testing.T) *AccountOutboundService {
	t.Helper()
	svc := &AccountOutboundService{
		downUntil: make(map[string]time.Time),
		relaySnap: cachedRelaySnapshot{
			url:     "http://relay.example:38480",
			host:    "relay.example:38480",
			ttl:     defaultTrafficRelayUnavailableTTL,
			expires: time.Now().Add(time.Minute),
		},
	}
	SetProcessAccountOutbound(svc)
	t.Cleanup(func() { SetProcessAccountOutbound(nil) })
	return svc
}

func relayTestRequest(t *testing.T, ctx context.Context, account *Account) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/chat/completions",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test")
	return WithAccountOutbound(req, account, AccountDefaultProxyURL(account))
}

func TestRelayHTTPUpstreamRetriesConnectOnceWithReplayableRequest(t *testing.T) {
	installRelayTestService(t)
	proxyID := int64(3)
	account := &Account{
		ID:            42,
		UseRelayRoute: true,
		ProxyID:       &proxyID,
		Proxy:         &Proxy{Protocol: "http", Host: "fallback.example", Port: 3128},
	}
	req := relayTestRequest(t, context.Background(), account)
	upstream := &relayTestUpstream{relayError: errors.New("proxyconnect tcp: dial tcp relay.example:38480: i/o timeout")}

	resp, err := WrapRelayHTTPUpstream(upstream).DoWithTLS(req, AccountDefaultProxyURL(account), account.ID, 1, nil)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response=%v err=%v", resp, err)
	}
	if got, want := strings.Join(upstream.calls, ","), "http://relay.example:38480,http://fallback.example:3128"; got != want {
		t.Fatalf("routes = %q, want %q", got, want)
	}
	if upstream.body != `{"model":"test"}` {
		t.Fatalf("fallback body = %q", upstream.body)
	}
	if !processOutbound().isDown(context.Background(), "http://relay.example:38480") {
		t.Fatal("relay outage was not recorded")
	}
}

func TestRelayHTTPUpstreamDoesNotRetryOriginTLSFailure(t *testing.T) {
	installRelayTestService(t)
	account := &Account{ID: 9, UseRelayRoute: true}
	req := relayTestRequest(t, context.Background(), account)
	upstream := &relayTestUpstream{relayError: errors.New("remote error: tls: handshake failure")}

	_, err := WrapRelayHTTPUpstream(upstream).Do(req, "", account.ID, 1)
	if err == nil || len(upstream.calls) != 1 {
		t.Fatalf("err=%v routes=%v", err, upstream.calls)
	}
}

func TestRelayHTTPUpstreamDoesNotRetryCancelledRequest(t *testing.T) {
	installRelayTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	account := &Account{ID: 11, UseRelayRoute: true}
	req := relayTestRequest(t, ctx, account)
	upstream := &relayTestUpstream{relayError: errors.New("proxyconnect tcp: i/o timeout")}

	_, err := WrapRelayHTTPUpstream(upstream).Do(req, "", account.ID, 1)
	if err == nil || len(upstream.calls) != 1 {
		t.Fatalf("err=%v routes=%v", err, upstream.calls)
	}
}

func TestWithAccountOutboundPreservesExplicitProxyOverride(t *testing.T) {
	installRelayTestService(t)
	account := &Account{ID: 12, UseRelayRoute: true}
	req, err := http.NewRequest(http.MethodGet, "https://upstream.example", nil)
	if err != nil {
		t.Fatal(err)
	}

	bound := WithAccountOutbound(req, account, "http://explicit.example:8080")
	route, ok := requestOutbound(bound, account.ID)
	if !ok || route.ViaRelay || route.ProxyURL != "http://explicit.example:8080" {
		t.Fatalf("explicit route changed: %+v, bound=%v", route, ok)
	}
}

func TestDialAccountWebSocketFallsBackOnlyBeforeHandshake(t *testing.T) {
	installRelayTestService(t)
	proxyID := int64(3)
	account := &Account{
		ID:            55,
		UseRelayRoute: true,
		ProxyID:       &proxyID,
		Proxy:         &Proxy{Protocol: "http", Host: "fallback.example", Port: 3128},
	}
	dialer := &relayTestWSDialer{}

	conn, status, headers, proxyURL, err := dialAccountWebSocket(
		context.Background(), dialer, account, "wss://upstream.example/ws", nil, AccountDefaultProxyURL(account),
	)
	if err != nil || conn == nil || status != http.StatusSwitchingProtocols {
		t.Fatalf("conn=%v status=%d err=%v", conn, status, err)
	}
	if got, want := strings.Join(dialer.calls, ","), "http://relay.example:38480,http://fallback.example:3128"; got != want {
		t.Fatalf("routes = %q, want %q", got, want)
	}
	if proxyURL != "http://fallback.example:3128" || headers.Get("X-Test") != "fallback" {
		t.Fatalf("fallback result proxy=%q headers=%v", proxyURL, headers)
	}
}

func TestDialAccountWebSocketDoesNotRetryRejectedHandshake(t *testing.T) {
	installRelayTestService(t)
	account := &Account{ID: 56, UseRelayRoute: true}
	dialer := &relayTestWSDialer{
		relayStatus: http.StatusForbidden,
		relayError:  errors.New("websocket handshake rejected"),
	}

	_, status, _, proxyURL, err := dialAccountWebSocket(
		context.Background(), dialer, account, "wss://upstream.example/ws", nil, "",
	)
	if err == nil || status != http.StatusForbidden || proxyURL != "http://relay.example:38480" {
		t.Fatalf("status=%d proxy=%q err=%v", status, proxyURL, err)
	}
	if len(dialer.calls) != 1 {
		t.Fatalf("handshake rejection was retried: %v", dialer.calls)
	}
}

func TestOpenAIWSCompatibilityIncludesOutboundRoute(t *testing.T) {
	account := &Account{ID: 57}
	headers := http.Header{"X-Codex-Beta-Features": []string{"responses_websockets=2026-02-06"}}
	relayKey := normalizeOpenAIWSHandshakeCompatibility(account, headers, "http://relay.example:38480")
	fallbackKey := normalizeOpenAIWSHandshakeCompatibility(account, headers, "http://fallback.example:3128")
	if relayKey == fallbackKey {
		t.Fatal("connections from different outbound routes were considered compatible")
	}
	if got := normalizeOpenAIWSHandshakeCompatibility(account, nil, ""); got != (openAIWSHandshakeCompatibilityKey{}) {
		t.Fatalf("direct connections lost the default compatibility key: %+v", got)
	}
}
