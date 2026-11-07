package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func fixture() configuration {
	return configuration{Accounts: map[string]credential{identityRef("test-user"): {UID: "test-user", AccessToken: "test-access", RefreshToken: "test-refresh", Domain: "codebuddy.cn", ExpiresAt: time.Now().Add(time.Hour).Unix(), Models: []string{"deepseek-v4-pro"}}}, Sessions: map[string]loginSession{}}
}
func rawConfig(c configuration) []byte { b, _ := json.Marshal(c); return b }

type forwardStream struct {
	ctx       context.Context
	requests  []*pluginv1.ForwardRequest
	responses []*pluginv1.ForwardResponse
}

func (s *forwardStream) Context() context.Context     { return s.ctx }
func (s *forwardStream) SetHeader(metadata.MD) error  { return nil }
func (s *forwardStream) SendHeader(metadata.MD) error { return nil }
func (s *forwardStream) SetTrailer(metadata.MD)       {}
func (s *forwardStream) SendMsg(any) error            { return nil }
func (s *forwardStream) RecvMsg(any) error            { return nil }
func (s *forwardStream) Send(r *pluginv1.ForwardResponse) error {
	s.responses = append(s.responses, r)
	return nil
}
func (s *forwardStream) Recv() (*pluginv1.ForwardRequest, error) {
	if len(s.requests) == 0 {
		return nil, io.EOF
	}
	r := s.requests[0]
	s.requests = s.requests[1:]
	return r, nil
}
func callStream(body string, ref string) *forwardStream {
	return &forwardStream{ctx: context.Background(), requests: []*pluginv1.ForwardRequest{
		{Frame: &pluginv1.ForwardRequest_Start{Start: &pluginv1.ForwardRequestStart{Method: "POST", Platform: "deepseek", AccountType: "plugin", IdentityRef: ref}}},
		{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: []byte(body)}},
		{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}},
	}}
}
func TestAuthorizationKeepsSecretsPrivateAndCatalogScoped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			io.WriteString(w, `{"code":0,"data":{"state":"test-state","authUrl":"https://www.codebuddy.cn/login?state=test-state"}}`)
		case "/v2/plugin/auth/token":
			require.Equal(t, "test-state", r.URL.Query().Get("state"))
			io.WriteString(w, `{"code":0,"data":{"accessToken":"test-access","refreshToken":"test-refresh","domain":"codebuddy.cn","expiresIn":3600}}`)
		case "/v2/plugin/login/account":
			require.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
			io.WriteString(w, `{"code":0,"data":{"uid":"test-user"}}`)
		case "/v2/enterprises/personal/models":
			io.WriteString(w, `{"code":0,"data":{"models":[{"id":"deepseek-v4-pro"},{"id":"gpt-other"}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := newServer()
	s.billing = upstream.URL
	s.chat = upstream.URL
	begin, e := s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{Action: "begin", ConfigJson: []byte(`{}`)})
	require.NoError(t, e)
	var public struct {
		SessionID string `json:"session_id"`
	}
	require.NoError(t, json.Unmarshal(begin.PublicJson, &public))
	require.NotEmpty(t, public.SessionID)
	poll, e := s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{Action: "poll", SessionId: public.SessionID, ConfigJson: begin.ConfigJson})
	require.NoError(t, e)
	require.True(t, poll.ConfigChanged)
	require.NotContains(t, string(poll.PublicJson), "test-access")
	require.NotContains(t, string(poll.PublicJson), "test-refresh")
	require.NotContains(t, string(poll.PublicJson), "test-user")
	require.NotContains(t, string(poll.PublicJson), "gpt-other")
	c, e := parseConfig(poll.ConfigJson)
	require.NoError(t, e)
	require.Len(t, c.Accounts, 1)
	require.Empty(t, c.Sessions)
	_, e = s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{Action: "poll", SessionId: public.SessionID, ConfigJson: poll.ConfigJson})
	require.Error(t, e)
}
func TestForwardPreservesToolReasoningAndUsageWithoutCredentialLeak(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[streaming], func(t *testing.T) {
			response := `{"id":"test","choices":[{"message":{"content":"answer","reasoning_content":"reason","tool_calls":[{"id":"call_test","type":"function","function":{"name":"test","arguments":"{}"}}]}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80}}}`
			if streaming {
				response = "data: " + response + "\n\ndata: [DONE]\n\n"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v2/chat/completions", r.URL.Path)
				require.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Refresh-Token"))
				require.Empty(t, r.Header.Get("Cookie"))
				raw, _ := io.ReadAll(r.Body)
				require.Contains(t, string(raw), `"reasoning_content":"old"`)
				contentType := "application/json"
				if streaming {
					contentType = "text/event-stream"
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Set-Cookie", "do-not-leak")
				io.WriteString(w, response)
			}))
			defer upstream.Close()
			s := newServer()
			s.chat = upstream.URL
			s.config = fixture()
			body := `{"model":"deepseek-v4-pro","messages":[{"role":"assistant","content":"old","reasoning_content":"old"}],"stream":` + map[bool]string{true: "true", false: "false"}[streaming] + `}`
			call := callStream(body, identityRef("test-user"))
			require.NoError(t, s.Forward(call))
			require.NotNil(t, call.responses[0].GetStart())
			require.NotContains(t, call.responses[0].GetStart().Headers, "Set-Cookie")
			var actual strings.Builder
			for _, frame := range call.responses {
				actual.Write(frame.GetBodyChunk())
			}
			require.Equal(t, response, actual.String())
			require.NotNil(t, call.responses[len(call.responses)-1].GetEnd())
		})
	}
}
func TestUnknownIdentityModelAndUnsupportedParametersNeverReachUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected upstream request") }))
	defer upstream.Close()
	s := newServer()
	s.chat = upstream.URL
	s.config = fixture()
	for _, test := range []struct{ body, ref string }{
		{`{"model":"deepseek-v4-pro"}`, "unknown"},
		{`{"model":"gpt-other"}`, identityRef("test-user")},
		{`{"model":"deepseek-v4-pro","background":true}`, identityRef("test-user")},
	} {
		call := callStream(test.body, test.ref)
		require.NoError(t, s.Forward(call))
		require.NotNil(t, call.responses[0].GetError())
		require.False(t, call.responses[0].GetError().RequestSent)
	}
}
func TestRefreshTokenOnlyReachesRefreshEndpoint(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/v2/plugin/auth/token/refresh", r.URL.Path)
		require.Equal(t, "test-refresh", r.Header.Get("X-Refresh-Token"))
		io.WriteString(w, `{"code":0,"data":{"accessToken":"test-rotated","refreshToken":"test-refresh-rotated","expiresIn":3600}}`)
	}))
	defer upstream.Close()
	s := newServer()
	s.chat = upstream.URL
	c := fixture()
	a := c.Accounts[identityRef("test-user")]
	a.ExpiresAt = 0
	c.Accounts[identityRef("test-user")] = a
	result, e := s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{Action: "prepare", IdentityRef: identityRef("test-user"), ConfigJson: rawConfig(c)})
	require.NoError(t, e)
	require.True(t, result.ConfigChanged)
	require.Equal(t, 1, calls)
	require.NotContains(t, string(result.PublicJson), "test-rotated")
	result, e = s.Authorize(context.Background(), &pluginv1.AuthorizeRequest{Action: "prepare", IdentityRef: identityRef("test-user"), ConfigJson: result.ConfigJson})
	require.NoError(t, e)
	require.False(t, result.ConfigChanged)
	require.Equal(t, 1, calls)
}
func TestRedirectCannotExfiltrateCredentials(t *testing.T) {
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("credentials followed redirect") }))
	defer sink.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	s := newServer()
	s.chat = upstream.URL
	s.config = fixture()
	call := callStream(`{"model":"deepseek-v4-pro"}`, identityRef("test-user"))
	require.NoError(t, s.Forward(call))
	require.EqualValues(t, 502, call.responses[0].GetStart().StatusCode)
}

func TestBusinessErrorsAreNeverSuccessfulUsage(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[streaming], func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"code\":14018,\"message\":\"test-private-upstream-detail\"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"code":14018,"message":"test-private-upstream-detail"}`)
				}
			}))
			defer upstream.Close()
			s := newServer()
			s.chat = upstream.URL
			s.config = fixture()
			call := callStream(`{"model":"deepseek-v4-pro"}`, identityRef("test-user"))
			require.NoError(t, s.Forward(call))
			require.Equal(t, 1, calls)
			raw, _ := json.Marshal(call.responses)
			require.NotContains(t, string(raw), "test-private-upstream-detail")
			if streaming {
				last := call.responses[len(call.responses)-1].GetError()
				require.NotNil(t, last)
				require.Equal(t, "quota_exhausted", last.Code)
				require.True(t, last.RequestSent)
			} else {
				require.EqualValues(t, 402, call.responses[0].GetStart().StatusCode)
			}
		})
	}
}

func TestForwardUsesOnlyTheExplicitlyBoundIdentity(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "test-user-b", r.Header.Get("X-User-Id"))
		require.Equal(t, "Bearer test-access-b", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Refresh-Token"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer upstream.Close()
	s := newServer()
	s.chat = upstream.URL
	s.config = fixture()
	s.config.Accounts[identityRef("test-user-b")] = credential{UID: "test-user-b", AccessToken: "test-access-b", RefreshToken: "test-refresh-b", Domain: "codebuddy.cn", Models: []string{"deepseek-v4-pro"}}
	call := callStream(`{"model":"deepseek-v4-pro"}`, identityRef("test-user-b"))
	require.NoError(t, s.Forward(call))
	require.Equal(t, 1, calls)
	require.EqualValues(t, 200, call.responses[0].GetStart().StatusCode)
}
