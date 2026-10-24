package service

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

func resolveRequestedAccountOutbound(ctx context.Context, account *Account, proxyURL string) AccountOutbound {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if svc := processOutbound(); svc != nil && account != nil && account.UseRelayRoute {
		relayURL, _ := svc.relayURL(ctx)
		if proxyURL == AccountDefaultProxyURL(account) || (relayURL != "" && proxyURL == relayURL) {
			return svc.Resolve(ctx, account)
		}
	}
	return AccountOutbound{ProxyURL: proxyURL}
}

// Dial fallback is only allowed before a WebSocket handshake succeeds.
func dialAccountWebSocket(ctx context.Context, dialer openAIWSClientDialer, account *Account,
	wsURL string, headers http.Header, proxyURL string,
) (openAIWSClientConn, int, http.Header, string, error) {
	route := resolveRequestedAccountOutbound(ctx, account, proxyURL)
	relayCtx := ctx
	if route.ViaRelay {
		relayCtx = proxyutil.WithRelayConnectHost(ctx, route.RelayHost)
	}
	conn, status, responseHeaders, err := dialer.Dial(relayCtx, wsURL, headers, route.ProxyURL)
	if conn == nil && status == 0 {
		err = markRelayRouteFailure(err, route)
	}
	if err == nil || conn != nil || status != 0 || ctx.Err() != nil ||
		!route.ViaRelay || !IsRelayConnectError(err, route.RelayHost) {
		return conn, status, responseHeaders, route.ProxyURL, err
	}
	MarkRelayDown(ctx, route)
	if route.FallbackProxyURL == route.ProxyURL {
		return conn, status, responseHeaders, route.ProxyURL, err
	}
	conn, status, responseHeaders, err = dialer.Dial(ctx, wsURL, headers, route.FallbackProxyURL)
	return conn, status, responseHeaders, route.FallbackProxyURL, err
}

// Relay and fallback sockets must not share the same pool identity. Busy
// sockets retain their lease; the existing pool replacement logic drains them.
func accountWSCompatibilityKey(account *Account, headers http.Header, proxyURL string) string {
	key := normalizeOpenAIWSBetaFeatures(headers)
	if account != nil && account.UseRelayRoute {
		key += "|" + relayDownKey(proxyURL)
	}
	return key
}
