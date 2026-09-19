package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type relayAntigravityRoundTrip func(*http.Request) (*http.Response, error)

func (f relayAntigravityRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRelayAntigravityPollingRetriesOnlyFailedRequest(t *testing.T) {
	svc := installTestRelay(t)
	ctx := withAccountOperationRoute(context.Background(), &Account{UseRelayRoute: true}, "")
	var relayCalls, directCalls int
	transport := &accountAntigravityTransport{routes: map[string]http.RoundTripper{
		svc.relaySnap.url: relayAntigravityRoundTrip(func(r *http.Request) (*http.Response, error) {
			relayCalls++
			if relayCalls == 2 {
				return nil, errors.New("proxyconnect tcp: i/o timeout")
			}
			return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
		}),
		"": relayAntigravityRoundTrip(func(r *http.Request) (*http.Response, error) {
			directCalls++
			body, _ := io.ReadAll(r.Body)
			if string(body) != "second-poll" {
				t.Errorf("replayed earlier operation: %s", body)
			}
			return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
		}),
	}}
	for _, body := range []string{"first-poll", "second-poll"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", strings.NewReader(body))
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if relayCalls != 2 || directCalls != 1 {
		t.Fatalf("relay=%d direct=%d", relayCalls, directCalls)
	}
}
