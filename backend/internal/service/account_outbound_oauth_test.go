package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

type relayOpenAIClient struct {
	OpenAIOAuthClient
	call func(string) error
}

func (c relayOpenAIClient) RefreshTokenWithClientID(_ context.Context, _ string, proxy, _ string) (*openai.TokenResponse, error) {
	if err := c.call(proxy); err != nil {
		return nil, err
	}
	return &openai.TokenResponse{AccessToken: "token", ExpiresIn: 3600}, nil
}

type relayClaudeClient struct {
	ClaudeOAuthClient
	call func(string) error
}

func (c relayClaudeClient) RefreshToken(_ context.Context, _, proxy string) (*oauth.TokenResponse, error) {
	if err := c.call(proxy); err != nil {
		return nil, err
	}
	return &oauth.TokenResponse{AccessToken: "token", ExpiresIn: 3600}, nil
}

type relayGeminiClient struct {
	GeminiOAuthClient
	call func(string) error
}

func (c relayGeminiClient) RefreshToken(_ context.Context, _, _, proxy string) (*geminicli.TokenResponse, error) {
	if err := c.call(proxy); err != nil {
		return nil, err
	}
	return &geminicli.TokenResponse{AccessToken: "token", ExpiresIn: 3600}, nil
}

type relayGrokClient struct {
	GrokOAuthClient
	call func(string) error
}

func (c relayGrokClient) RefreshToken(_ context.Context, _, proxy, _ string) (*xai.TokenResponse, error) {
	if err := c.call(proxy); err != nil {
		return nil, err
	}
	return &xai.TokenResponse{AccessToken: "token", ExpiresIn: 3600}, nil
}

func TestAccountRelayOAuthRefreshPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			svc := installTestRelay(t)
			var calls []string
			call := func(proxy string) error {
				calls = append(calls, proxy)
				if proxy == svc.relaySnap.url {
					return errors.New("proxyconnect tcp: i/o timeout")
				}
				return nil
			}
			id := int64(1)
			proxy := &Proxy{ID: id, Protocol: "http", Host: "default-proxy", Port: 3128, Status: "active"}
			repo := relaySettingsProxyRepo{proxy: proxy}
			account := &Account{ID: 7, Platform: platform, Type: AccountTypeOAuth,
				UseRelayRoute: true, ProxyID: &id, Credentials: map[string]any{
					"refresh_token": "refresh", "oauth_type": "ai_studio",
				}}
			var err error
			switch platform {
			case PlatformOpenAI:
				_, err = (&OpenAIOAuthService{proxyRepo: repo, oauthClient: relayOpenAIClient{call: call}}).RefreshAccountToken(context.Background(), account)
			case PlatformAnthropic:
				_, err = (&OAuthService{proxyRepo: repo, oauthClient: relayClaudeClient{call: call}}).RefreshAccountToken(context.Background(), account)
			case PlatformGemini:
				_, err = (&GeminiOAuthService{proxyRepo: repo, oauthClient: relayGeminiClient{call: call}}).RefreshAccountToken(context.Background(), account)
			case PlatformGrok:
				_, err = (&GrokOAuthService{proxyRepo: repo, oauthClient: relayGrokClient{call: call}}).RefreshAccountToken(context.Background(), account)
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{svc.relaySnap.url, proxy.URL()}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
			if account.Proxy != nil {
				t.Fatal("shared account relation was modified")
			}
		})
	}
}
