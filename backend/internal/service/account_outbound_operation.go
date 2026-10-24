package service

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/imroc/req/v3"
)

type accountOperationRouteKey struct{}

// Bind the already-loaded default route without mutating a shared account.
func withAccountOperationRoute(ctx context.Context, account *Account, fallback string) context.Context {
	route := AccountOutbound{ProxyURL: fallback}
	if svc := processOutbound(); svc != nil && account != nil && account.UseRelayRoute {
		route = svc.Resolve(ctx, account)
		if route.ViaRelay {
			route.FallbackProxyURL = fallback
			ctx = proxyutil.WithRelayConnectHost(ctx, route.RelayHost)
		} else {
			route.ProxyURL = fallback
		}
	}
	return context.WithValue(ctx, accountOperationRouteKey{}, route)
}

// Apply this to a single network operation, never a multi-step token workflow:
// replaying the workflow could reuse an already rotated refresh token.
func accountOutboundOperation[T any](ctx context.Context, fallback string, call func(string) (T, error)) (T, string, error) {
	route, ok := ctx.Value(accountOperationRouteKey{}).(AccountOutbound)
	if !ok {
		route = AccountOutbound{ProxyURL: fallback}
	}
	if route.ViaRelay {
		if svc := processOutbound(); svc != nil {
			checkCtx, cancel := context.WithTimeout(ctx, time.Second)
			down := svc.isDown(checkCtx, route.ProxyURL)
			cancel()
			if down {
				route.ProxyURL = route.FallbackProxyURL
				route.ViaRelay = false
			}
		}
	}
	if ctx.Err() != nil {
		var zero T
		return zero, route.ProxyURL, ctx.Err()
	}
	value, err := call(route.ProxyURL)
	if response, ok := any(value).(*http.Response); ok && response != nil {
		return value, route.ProxyURL, err
	}
	if response, ok := any(value).(*req.Response); ok && response != nil && response.Response != nil {
		return value, route.ProxyURL, err
	}
	err = markRelayRouteFailure(err, route)
	if err == nil || ctx.Err() != nil || !route.ViaRelay || !IsRelayConnectError(err, route.RelayHost) {
		return value, route.ProxyURL, err
	}
	MarkRelayDown(ctx, route)
	if route.ProxyURL == route.FallbackProxyURL {
		return value, route.ProxyURL, err
	}
	value, err = call(route.FallbackProxyURL)
	return value, route.FallbackProxyURL, err
}

func accountPrivacyOperation(ctx context.Context, factory PrivacyClientFactory, fallback string, call func(*req.Client) (*req.Response, error)) (*req.Response, error) {
	response, _, err := accountOutboundOperation(ctx, fallback, func(proxy string) (*req.Response, error) {
		client, err := factory(proxy)
		if err != nil {
			return nil, err
		}
		return call(client)
	})
	return response, err
}
