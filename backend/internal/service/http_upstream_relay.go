package service

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type relayAwareHTTPUpstream struct {
	inner HTTPUpstream
}

// WrapRelayHTTPUpstream retries CONNECT failures on the account default route.
func WrapRelayHTTPUpstream(inner HTTPUpstream) HTTPUpstream {
	if inner == nil {
		return nil
	}
	return relayAwareHTTPUpstream{inner: inner}
}

func (u relayAwareHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if route, ok := requestOutbound(req, accountID); ok {
		proxyURL = route.ProxyURL
	}
	resp, err := u.inner.Do(req, proxyURL, accountID, accountConcurrency)
	if err == nil {
		return resp, nil
	}
	// A response means the upstream has started answering; never replay it.
	if resp != nil {
		return resp, err
	}
	if route, ok := requestOutbound(req, accountID); ok {
		err = markRelayRouteFailure(err, route)
	}
	resp, err, retried := RetryRelayHTTP(requestContext(req), accountID, proxyURL, req, err, func(next *http.Request, fallback string) (*http.Response, error) {
		return u.inner.Do(next, fallback, accountID, accountConcurrency)
	})
	if retried {
		return resp, err
	}
	return nil, err
}

func (u relayAwareHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if route, ok := requestOutbound(req, accountID); ok {
		proxyURL = route.ProxyURL
	}
	resp, err := u.inner.DoWithTLS(req, proxyURL, accountID, accountConcurrency, profile)
	if err == nil {
		return resp, nil
	}
	if resp != nil {
		return resp, err
	}
	if route, ok := requestOutbound(req, accountID); ok {
		err = markRelayRouteFailure(err, route)
	}
	resp, err, retried := RetryRelayHTTP(requestContext(req), accountID, proxyURL, req, err, func(next *http.Request, fallback string) (*http.Response, error) {
		return u.inner.DoWithTLS(next, fallback, accountID, accountConcurrency, profile)
	})
	if retried {
		return resp, err
	}
	return nil, err
}

func requestContext(req *http.Request) context.Context {
	if req == nil || req.Context() == nil {
		return context.Background()
	}
	return req.Context()
}
