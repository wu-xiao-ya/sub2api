package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
)

func TestRelayWebSocketStalledConnectFallsBack(t *testing.T) {
	var attempts atomic.Int32
	closed := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(closed)
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer proxy.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	}))
	defer origin.Close()
	svc := installTestRelay(t)
	svc.relaySnap.url = proxy.URL
	svc.relaySnap.host = proxy.Listener.Addr().String()
	account := &Account{ID: 8, UseRelayRoute: true}
	dialer := newDefaultOpenAIWSClientDialer()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, _, _, used, err := dialAccountWebSocket(ctx, dialer, account,
			"ws"+strings.TrimPrefix(origin.URL, "http"), nil, proxy.URL)
		if err != nil || conn == nil {
			cancel()
			t.Fatalf("fallback failed: %v", err)
		}
		if used != "" || ctx.Err() != nil {
			t.Errorf("default route unavailable: used=%q context=%v", used, ctx.Err())
		}
		_ = conn.Close()
		cancel()
	}
	if attempts.Load() != 1 {
		t.Fatalf("cooldown failed, relay attempts=%d", attempts.Load())
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("stalled CONNECT socket leaked")
	}
}

type testRelayWSDialer struct {
	proxies []string
	status  int
	err     error
}

func (d *testRelayWSDialer) Dial(_ context.Context, _ string, _ http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.proxies = append(d.proxies, proxy)
	if len(d.proxies) == 1 {
		return nil, d.status, nil, d.err
	}
	return nil, http.StatusSwitchingProtocols, nil, nil
}

func installTestRelay(t *testing.T) *AccountOutboundService {
	t.Helper()
	svc := &AccountOutboundService{relaySnap: cachedRelaySnapshot{
		url: "http://relay.example:38480", host: "relay.example:38480",
		ttl: defaultTrafficRelayUnavailableTTL, expires: time.Now().Add(time.Minute),
	}}
	old := processOutbound()
	SetProcessAccountOutbound(svc)
	t.Cleanup(func() { SetProcessAccountOutbound(old) })
	return svc
}

func TestRelayWebSocketFallsBackToOwnProxy(t *testing.T) {
	svc := installTestRelay(t)
	id := int64(1)
	account := &Account{ID: 8, UseRelayRoute: true, ProxyID: &id,
		Proxy: &Proxy{Protocol: "http", Host: "own-proxy", Port: 3128}}
	dialer := &testRelayWSDialer{err: errors.New("proxyconnect tcp: i/o timeout")}
	_, _, _, used, err := dialAccountWebSocket(context.Background(), dialer, account,
		"wss://example.test", nil, svc.relaySnap.url)
	want := []string{svc.relaySnap.url, AccountDefaultProxyURL(account)}
	if err != nil || !reflect.DeepEqual(dialer.proxies, want) || used != want[1] {
		t.Fatalf("proxies=%v used=%q err=%v", dialer.proxies, used, err)
	}
	if next := svc.Resolve(context.Background(), account); next.ViaRelay || next.ProxyURL != want[1] {
		t.Fatalf("cooldown did not preserve default proxy: %+v", next)
	}
	if svc.isDown(context.Background(), "http://new-relay:38480") {
		t.Fatal("old outage must not affect a new relay configuration")
	}
}

func TestRelayWebSocketDoesNotRetryOriginErrors(t *testing.T) {
	svc := installTestRelay(t)
	for _, test := range []struct {
		status int
		err    error
	}{
		{http.StatusUnauthorized, errors.New("unauthorized")},
		{http.StatusTooManyRequests, errors.New("rate limited")},
		{0, errors.New("tls: handshake failure")},
		{0, errors.New("proxyconnect tcp: Bad Gateway")},
	} {
		dialer := &testRelayWSDialer{status: test.status, err: test.err}
		_, _, _, _, err := dialAccountWebSocket(context.Background(), dialer,
			&Account{ID: 8, UseRelayRoute: true}, "wss://example.test", nil, svc.relaySnap.url)
		if err == nil || len(dialer.proxies) != 1 {
			t.Fatalf("origin error retried: status=%d proxies=%v", test.status, dialer.proxies)
		}
	}
}

func TestRelayWebSocketPoolIdentity(t *testing.T) {
	account := &Account{UseRelayRoute: true}
	relay := accountWSCompatibilityKey(account, nil, "http://relay:38480")
	direct := accountWSCompatibilityKey(account, nil, "")
	if relay == direct {
		t.Fatal("relay and direct sockets must be isolated")
	}
	account.UseRelayRoute = false
	if got := accountWSCompatibilityKey(account, nil, "http://relay:38480"); got != normalizeOpenAIWSBetaFeatures(nil) {
		t.Fatal("unchecked account pool semantics changed")
	}
}
