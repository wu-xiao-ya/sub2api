package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/imroc/req/v3"
)

func TestRelayQuotaOperationsPreserveDefaultAndResetID(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "query"
		if reset {
			name = "reset"
		}
		t.Run(name, func(t *testing.T) {
			relay := installTestRelay(t)
			proxyID := int64(7)
			account := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				UseRelayRoute: true, ProxyID: &proxyID,
				Proxy:       &Proxy{Protocol: "http", Host: "own-proxy", Port: 3128},
				Credentials: map[string]any{"chatgpt_account_id": "test-account"}}
			repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{100: account}}
			cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "test-token"}}
			var resetID string
			var originCalls int
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originCalls++
				w.Header().Set("Content-Type", "application/json")
				if reset {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if resetID == "" || body["redeem_request_id"] != resetID {
						t.Error("fallback changed redeem_request_id")
					}
					_, _ = w.Write([]byte(`{"code":"ok","windows_reset":1}`))
				} else {
					_, _ = w.Write([]byte(`{"rate_limit_reset_credits":{"available_count":1},"credits":[]}`))
				}
			}))
			defer origin.Close()
			var routes []string
			factory := func(proxy string) (*req.Client, error) {
				routes = append(routes, proxy)
				if proxy != relay.relaySnap.url {
					return newQuotaRedirectingFactory(origin)(proxy)
				}
				return req.C().WrapRoundTripFunc(func(req.RoundTripper) req.RoundTripFunc {
					return func(r *req.Request) (*req.Response, error) {
						if reset {
							var body map[string]string
							if err := json.Unmarshal(r.Body, &body); err != nil {
								t.Fatalf("invalid reset body: %v", err)
							}
							resetID = body["redeem_request_id"]
						}
						return &req.Response{}, errors.New("proxyconnect tcp: i/o timeout")
					}
				}), nil
			}
			svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), factory)
			var err error
			expectedCalls := 2
			wantRoutes := []string{relay.relaySnap.url, AccountDefaultProxyURL(account), AccountDefaultProxyURL(account)}
			if reset {
				_, err = svc.ResetCredit(context.Background(), account.ID)
				expectedCalls = 1
				wantRoutes = wantRoutes[:2]
			} else {
				_, err = svc.QueryUsage(context.Background(), account.ID)
			}
			if err != nil || originCalls != expectedCalls || !reflect.DeepEqual(routes, wantRoutes) {
				t.Fatalf("calls=%d routes=%v err=%v", originCalls, routes, err)
			}
		})
	}
}
