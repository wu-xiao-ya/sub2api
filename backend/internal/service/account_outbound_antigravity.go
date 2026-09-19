package service

import (
	"context"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

type accountAntigravityTransport struct {
	fallback string
	mu       sync.Mutex
	routes   map[string]http.RoundTripper
}

// Bind fallback at the individual HTTP request boundary, including onboarding
// polling. A later poll failure must never replay an earlier successful POST.
func newAccountAntigravityClient(ctx context.Context, fallback string) (*antigravity.Client, error) {
	if _, ok := ctx.Value(accountOperationRouteKey{}).(AccountOutbound); !ok {
		return antigravity.NewClient(fallback)
	}
	return antigravity.NewClient(fallback, func(base http.RoundTripper) http.RoundTripper {
		return &accountAntigravityTransport{fallback: fallback, routes: map[string]http.RoundTripper{fallback: base}}
	})
}

func (t *accountAntigravityTransport) transport(route string) (http.RoundTripper, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cached := t.routes[route]; cached != nil {
		return cached, nil
	}
	client, err := antigravity.NewHTTPClient(route)
	if err != nil {
		return nil, err
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	t.routes[route] = base
	return base, nil
}

func (t *accountAntigravityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	first := true
	resp, _, err := accountOutboundOperation(req.Context(), t.fallback, func(route string) (*http.Response, error) {
		attempt := req
		if !first {
			var err error
			attempt, err = cloneHTTPRequest(req)
			if err != nil {
				return nil, err
			}
		}
		first = false
		transport, err := t.transport(route)
		if err != nil {
			return nil, err
		}
		return transport.RoundTrip(attempt)
	})
	return resp, err
}
