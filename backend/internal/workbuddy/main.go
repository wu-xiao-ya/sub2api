// WorkBuddy protocol reference: dingminhua/dsh-connect-workbuddy (MIT),
// d73db9816ffa54ea9bca1c1e0b68612abeac8e8c. See THIRD_PARTY_NOTICES.md.
package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ID = "org.starlight.workbuddy"
const pluginID = ID
const Version = "0.1.3"
const pluginVersion = Version
const capability = "upstream.chat_completions.v1"
const billingBase = "https://www.codebuddy.cn"
const chatBase = "https://copilot.tencent.com"
const maxBody = 32 << 20
const authorizationPendingCode = 11217

type metadataBusinessError struct {
	code float64
}

func (e *metadataBusinessError) Error() string {
	return "upstream metadata not ready or refused"
}

func (e *metadataBusinessError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

type credential struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	UID          string   `json:"uid"`
	Domain       string   `json:"domain"`
	EnterpriseID string   `json:"enterprise_id,omitempty"`
	ExpiresAt    int64    `json:"expires_at"`
	Models       []string `json:"models"`
}
type loginSession struct {
	State       string `json:"state"`
	IdentityRef string `json:"identity_ref,omitempty"`
	ExpiresAt   int64  `json:"expires_at"`
}
type configuration struct {
	Accounts map[string]credential   `json:"accounts"`
	Sessions map[string]loginSession `json:"sessions"`
}
type server struct {
	pluginv1.UnimplementedTransportPluginServer
	mu     sync.RWMutex
	config configuration
	client *http.Client
	// Set only by tests. Production endpoints are compiled-in and never configurable.
	billing string
	chat    string
	now     func() time.Time
}

func newServer() *server {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 60 * time.Second
	return &server{client: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, billing: billingBase, chat: chatBase, now: time.Now, config: configuration{Accounts: map[string]credential{}, Sessions: map[string]loginSession{}}}
}
func Serve() { pluginv1.Serve(newServer()) }
func parseConfig(raw []byte) (configuration, error) {
	var c configuration
	if len(raw) > 4<<20 || json.Unmarshal(raw, &c) != nil {
		return c, errors.New("invalid configuration")
	}
	if c.Accounts == nil {
		c.Accounts = map[string]credential{}
	}
	if c.Sessions == nil {
		c.Sessions = map[string]loginSession{}
	}
	for ref, a := range c.Accounts {
		if ref != identityRef(a.UID) || a.AccessToken == "" || a.UID == "" || !domesticDomain(a.Domain) {
			return c, errors.New("invalid domestic identity")
		}
		for _, model := range a.Models {
			if !isDeepSeek(model) {
				return c, errors.New("only DeepSeek models are allowed")
			}
		}
	}
	return c, nil
}
func domesticDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || domain == "copilot.tencent.com" || domain == "wb.tencentbuddy.com" {
		return true
	}
	for _, suffix := range []string{"workbuddy.cn", "codebuddy.cn"} {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return true
		}
	}
	return false
}
func identityRef(uid string) string {
	h := sha256.Sum256([]byte(uid))
	return "wb_" + hex.EncodeToString(h[:16])
}
func isDeepSeek(m string) bool { return strings.HasPrefix(strings.ToLower(m), "deepseek-") }
func (s *server) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: pluginID, PluginVersion: pluginVersion, ProtocolVersion: pluginv1.ProtocolVersion, TransportApiVersion: pluginv1.TransportAPIVersion, Capabilities: []string{capability}}, nil
}
func (s *server) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accounts := []map[string]any{}
	for ref, a := range s.config.Accounts {
		accounts = append(accounts, map[string]any{"identity_ref": ref, "models": a.Models, "expires_at": a.ExpiresAt})
	}
	raw, _ := json.Marshal(map[string]any{"accounts": accounts, "region": "cn", "device_sdk": "not_used", "live_acceptance": "required"})
	return &pluginv1.HealthResponse{Healthy: true, Message: "runtime ready; upstream acceptance requires an authorized test", StatusJson: string(raw)}, nil
}
func (s *server) ValidateConfig(_ context.Context, r *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	c, e := parseConfig(r.ConfigJson)
	if e != nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: e.Error()}, nil
	}
	raw, _ := json.Marshal(c)
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: raw}, nil
}
func (s *server) ApplyConfig(_ context.Context, r *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	c, e := parseConfig(r.ConfigJson)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, e.Error())
	}
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func (s *server) TestConfig(ctx context.Context, r *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	c, e := parseConfig(r.ConfigJson)
	if e != nil {
		return &pluginv1.TestConfigResponse{Message: e.Error()}, nil
	}
	if len(c.Accounts) == 0 {
		return &pluginv1.TestConfigResponse{Message: "authorize an account first"}, nil
	}
	for _, a := range c.Accounts {
		models, e := s.models(ctx, a)
		if e != nil || len(models) == 0 {
			return &pluginv1.TestConfigResponse{Message: "model catalog unavailable or no DeepSeek access"}, nil
		}
	}
	return &pluginv1.TestConfigResponse{Success: true, Message: "authorized catalogs available; inference and billing still require live acceptance"}, nil
}
func (s *server) Authorize(ctx context.Context, r *pluginv1.AuthorizeRequest) (*pluginv1.AuthorizeResponse, error) {
	c, e := parseConfig(r.ConfigJson)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid configuration")
	}
	now := s.now().Unix()
	for id, session := range c.Sessions {
		if session.ExpiresAt < now {
			delete(c.Sessions, id)
		}
	}
	public := map[string]any{}
	changed := false
	switch r.Action {
	case "import":
		var err error
		public, changed, err = s.importAccounts(ctx, c, r.SessionId)
		if err != nil {
			return nil, err
		}
	case "begin":
		if len(c.Sessions) >= 16 {
			return nil, status.Error(codes.ResourceExhausted, "too many pending authorizations")
		}
		if r.IdentityRef != "" {
			if _, ok := c.Accounts[r.IdentityRef]; !ok {
				return nil, status.Error(codes.NotFound, "identity not found")
			}
		}
		data, e := s.document(ctx, http.MethodPost, s.billing+"/v2/plugin/auth/state?platform=workbuddy", nil, []byte("{}"))
		if e != nil {
			return nil, e
		}
		state := str(data, "state", "State")
		if state == "" {
			return nil, status.Error(codes.Unavailable, "authorization state unavailable")
		}
		link := str(data, "authUrl", "auth_url", "url", "Url")
		if link == "" {
			link = s.billing + "/login?state=" + url.QueryEscape(state)
		}
		u, e := url.Parse(link)
		if e != nil || u.Scheme != "https" || u.Host != "www.codebuddy.cn" || u.User != nil {
			return nil, status.Error(codes.FailedPrecondition, "unexpected authorization URL")
		}
		id := randomID()
		c.Sessions[id] = loginSession{State: state, IdentityRef: r.IdentityRef, ExpiresAt: now + 600}
		changed = true
		public = map[string]any{"session_id": id, "verification_uri": link, "expires_in": 600}
	case "poll":
		session, ok := c.Sessions[r.SessionId]
		if !ok {
			public = map[string]any{"done": true, "reason": "session_expired"}
			break
		}
		data, e := s.document(ctx, http.MethodGet, s.billing+"/v2/plugin/auth/token?state="+url.QueryEscape(session.State), nil, nil)
		if e != nil {
			var refusal *metadataBusinessError
			switch {
			case errors.As(e, &refusal) && refusal.code == authorizationPendingCode:
				public = map[string]any{"done": false, "reason": "upstream_pending"}
			case refusal != nil:
				public = map[string]any{"done": false, "reason": "upstream_refused", "upstream_code": refusal.code}
			default:
				public = map[string]any{"done": false, "reason": "upstream_unavailable"}
			}
			break
		}
		token := str(data, "accessToken", "access_token", "AccessToken")
		if token == "" {
			public = map[string]any{"done": false, "reason": "missing_token"}
			break
		}
		a := credential{AccessToken: token, RefreshToken: str(data, "refreshToken", "refresh_token", "RefreshToken"), Domain: str(data, "domain", "Domain"), ExpiresAt: expiry(data, now)}
		profile, e := s.document(ctx, http.MethodGet, s.billing+"/v2/plugin/login/account", &a, nil)
		if e != nil {
			return nil, e
		}
		a.UID = str(profile, "uid", "Uid", "userId")
		a.EnterpriseID = str(profile, "enterpriseId", "enterprise_id", "EnterpriseId")
		if a.Domain == "" {
			a.Domain = str(profile, "domain", "Domain")
		}
		if !domesticDomain(a.Domain) {
			return nil, status.Error(codes.PermissionDenied, "domestic account required")
		}
		if a.UID == "" {
			return nil, status.Error(codes.FailedPrecondition, "account identity unavailable")
		}
		ref := identityRef(a.UID)
		if session.IdentityRef != "" && session.IdentityRef != ref {
			return nil, status.Error(codes.PermissionDenied, "reauthorization must use the same account")
		}
		a.Models, e = s.models(ctx, a)
		if e != nil {
			return nil, e
		}
		if len(a.Models) == 0 {
			return nil, status.Error(codes.FailedPrecondition, "no DeepSeek models available")
		}
		c.Accounts[ref] = a
		delete(c.Sessions, r.SessionId)
		changed = true
		public = map[string]any{"done": true, "authorized": true, "identity_ref": ref, "models": a.Models}
	case "cancel":
		delete(c.Sessions, r.SessionId)
		changed = true
		public["cancelled"] = true
	case "unlink":
		delete(c.Accounts, r.IdentityRef)
		changed = true
		public["unlinked"] = true
	case "credits":
		a, ok := c.Accounts[r.IdentityRef]
		if !ok {
			return nil, status.Error(codes.NotFound, "identity is not authorized")
		}
		credits, err := s.fetchCredits(ctx, a)
		if err != nil {
			return nil, err
		}
		public = credits
	case "inspect", "prepare":
		a, ok := c.Accounts[r.IdentityRef]
		if !ok {
			return nil, status.Error(codes.NotFound, "identity is not authorized")
		}
		if r.Action == "prepare" && a.ExpiresAt <= now+60 {
			if a.RefreshToken == "" {
				return nil, status.Error(codes.Unauthenticated, "reauthorization required")
			}
			data, e := s.refresh(ctx, a)
			if e != nil {
				return nil, e
			}
			a.AccessToken = str(data, "accessToken", "access_token")
			if a.AccessToken == "" {
				return nil, status.Error(codes.Unauthenticated, "reauthorization required")
			}
			if token := str(data, "refreshToken", "refresh_token"); token != "" {
				a.RefreshToken = token
			}
			a.ExpiresAt = expiry(data, now)
			if a.ExpiresAt <= now+60 {
				return nil, status.Error(codes.Unauthenticated, "token expiry unavailable; reauthorize")
			}
			c.Accounts[r.IdentityRef] = a
			changed = true
		}
		public = map[string]any{"identity_ref": r.IdentityRef, "authorized": true, "models": a.Models}
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported authorization action")
	}
	result, _ := json.Marshal(public)
	raw, _ := json.Marshal(c)
	return &pluginv1.AuthorizeResponse{PublicJson: result, ConfigJson: raw, ConfigChanged: changed}, nil
}
func randomID() string {
	var b [24]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func expiry(m map[string]any, now int64) int64 {
	for _, k := range []string{"expiresAt", "expires_at"} {
		if n, ok := m[k].(float64); ok && n > 0 {
			if n > 1e12 {
				n /= 1000
			}
			return int64(n)
		}
	}
	for _, k := range []string{"expiresIn", "expires_in"} {
		if n, ok := m[k].(float64); ok && n > 0 {
			return now + int64(n)
		}
	}
	return 0
}
func (s *server) request(ctx context.Context, method, target string, a *credential, body []byte) (*http.Response, error) {
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-WorkBuddy/"+pluginVersion)
	if a != nil {
		req.Header.Set("Authorization", "Bearer "+a.AccessToken)
		if a.UID != "" {
			req.Header.Set("X-User-Id", a.UID)
		}
		if a.Domain != "" {
			req.Header.Set("X-Domain", a.Domain)
		}
		if a.EnterpriseID != "" {
			req.Header.Set("X-Enterprise-Id", a.EnterpriseID)
			if req.URL.Path == "/billing/meter/get-enterprise-user-usage" {
				req.Header.Set("X-Tenant-Id", a.EnterpriseID)
			}
		}
	}
	resp, e := s.client.Do(req)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "upstream transport unavailable")
	}
	return resp, nil
}
func (s *server) document(ctx context.Context, method, target string, a *credential, body []byte) (map[string]any, error) {
	resp, e := s.request(ctx, method, target, a, body)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	return readDocument(resp)
}
func readDocument(resp *http.Response) (map[string]any, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, status.Errorf(codes.Unavailable, "upstream metadata refused (HTTP %d)", resp.StatusCode)
	}
	var envelope map[string]any
	if e := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope); e != nil {
		return nil, status.Error(codes.Unavailable, "invalid upstream metadata")
	}
	if code, ok := envelope["code"].(float64); ok && code != 0 && code != 200 {
		return nil, &metadataBusinessError{code: code}
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return nil, status.Error(codes.Unavailable, "invalid metadata envelope")
	}
	return data, nil
}
func (s *server) refresh(ctx context.Context, a credential) (map[string]any, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, s.chat+"/v2/plugin/auth/token/refresh", nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("X-Refresh-Token", a.RefreshToken)
	req.Header.Set("X-Auth-Refresh-Source", "workbuddy")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-WorkBuddy/"+pluginVersion)
	resp, e := s.client.Do(req)
	if e != nil {
		return nil, status.Error(codes.Unavailable, "token refresh unavailable")
	}
	defer resp.Body.Close()
	return readDocument(resp)
}
func (s *server) models(ctx context.Context, a credential) ([]string, error) {
	data, e := s.document(ctx, http.MethodGet, s.chat+"/v2/enterprises/personal/models", &a, nil)
	if e != nil {
		return nil, e
	}
	rows, ok := data["models"].([]any)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "unsupported catalog format")
	}
	models := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			id := str(m, "id")
			disabled, _ := m["disabled"].(bool)
			if !disabled && isDeepSeek(id) && !seen[id] {
				models = append(models, id)
				seen[id] = true
			}
		}
	}
	return models, nil
}
func (s *server) Forward(stream pluginv1.TransportPlugin_ForwardServer) error {
	first, e := stream.Recv()
	if e != nil {
		return e
	}
	start := first.GetStart()
	if start == nil || start.Platform != "deepseek" || start.AccountType != "plugin" || start.Method != http.MethodPost || start.IdentityRef == "" {
		return status.Error(codes.InvalidArgument, "invalid bound provider request")
	}
	s.mu.RLock()
	a, ok := s.config.Accounts[start.IdentityRef]
	s.mu.RUnlock()
	if !ok {
		return sendError(stream, "authorization_required", "reauthorization required", false)
	}
	var body bytes.Buffer
	for {
		frame, e := stream.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if chunk := frame.GetBodyChunk(); len(chunk) > 0 {
			if body.Len()+len(chunk) > maxBody {
				return status.Error(codes.ResourceExhausted, "request too large")
			}
			body.Write(chunk)
		}
	}
	var request map[string]any
	if json.Unmarshal(body.Bytes(), &request) != nil {
		return sendError(stream, "invalid_request", "invalid JSON", false)
	}
	model, _ := request["model"].(string)
	allowed := false
	for _, id := range a.Models {
		if id == model {
			allowed = true
		}
	}
	if !allowed || !isDeepSeek(model) {
		return sendError(stream, "model_not_allowed", "model not in authorized DeepSeek catalog", false)
	}
	for _, field := range []string{"previous_response_id", "store", "background", "web_search", "audio", "modalities"} {
		if _, ok := request[field]; ok {
			return sendError(stream, "unsupported_parameter", "unsupported parameter: "+field, false)
		}
	}
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, s.chat+"/v2/chat/completions", bytes.NewReader(body.Bytes()))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", a.UID)
	req.Header.Set("User-Agent", "Sub2API-WorkBuddy/"+pluginVersion)
	if a.Domain != "" {
		req.Header.Set("X-Domain", a.Domain)
	}
	if a.EnterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", a.EnterpriseID)
	}
	req.Header.Set("Accept", "application/json")
	if streamMode, _ := request["stream"].(bool); streamMode {
		req.Header.Set("Accept", "text/event-stream")
	}
	client := s.client
	if start.ProxyUrl != "" {
		proxy, e := url.Parse(start.ProxyUrl)
		if e != nil || (proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5" && proxy.Scheme != "socks5h") {
			return sendError(stream, "invalid_proxy", "unsupported proxy", false)
		}
		tr, ok := s.client.Transport.(*http.Transport)
		if !ok {
			return sendError(stream, "invalid_proxy", "transport cannot use proxy", false)
		}
		clone := tr.Clone()
		clone.Proxy = http.ProxyURL(proxy)
		defer clone.CloseIdleConnections()
		copy := *client
		copy.Transport = clone
		client = &copy
	}
	resp, e := client.Do(req)
	if e != nil {
		return sendError(stream, "upstream_transport", "upstream transport failed; request may have been sent", true)
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		code, _ := classifyFailure(resp.StatusCode, raw)
		return writeFailure(stream, resp.StatusCode, code)
	}
	if strings.Contains(contentType, "application/json") {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		if err != nil || len(raw) > maxBody {
			return sendError(stream, "invalid_upstream", "invalid upstream JSON response", true)
		}
		code, business := classifyFailure(resp.StatusCode, raw)
		if business {
			return writeFailure(stream, 502, code)
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil || doc["choices"] == nil {
			return writeFailure(stream, 502, "invalid_upstream_response")
		}
		if e = sendStart(stream, int32(resp.StatusCode), contentType); e != nil {
			return e
		}
		if e = sendBody(stream, raw); e != nil {
			return e
		}
		return sendEnd(stream)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return sendError(stream, "invalid_upstream", "unexpected upstream content type", true)
	}
	if e = sendStart(stream, int32(resp.StatusCode), contentType); e != nil {
		return e
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if code, business := classifyFailure(200, []byte(data)); business {
				return sendError(stream, code, "WorkBuddy rejected the stream", true)
			}
		}
		if e = sendBody(stream, []byte(line+"\n")); e != nil {
			return e
		}
	}
	if scanner.Err() != nil {
		return status.Error(codes.Unavailable, "upstream stream interrupted")
	}
	return sendEnd(stream)
}
func classifyFailure(httpStatus int, raw []byte) (string, bool) {
	code := "upstream_error"
	switch httpStatus {
	case 401:
		code = "authorization_required"
	case 402:
		code = "quota_exhausted"
	case 403:
		code = "upstream_rejected"
	case 429:
		code = "rate_limit_exceeded"
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) == nil {
		if n, ok := doc["code"].(float64); ok && n != 0 && n != 200 {
			switch int(n) {
			case 14018:
				code = "quota_exhausted"
			case 11128, 11140:
				code = "upstream_rejected"
			}
			return code, true
		}
		if _, ok := doc["error"]; ok {
			return code, true
		}
	}
	return code, httpStatus < 200 || httpStatus >= 300
}
func writeFailure(stream pluginv1.TransportPlugin_ForwardServer, httpStatus int, code string) error {
	switch code {
	case "quota_exhausted":
		httpStatus = 402
	case "authorization_required":
		httpStatus = 401
	case "upstream_rejected":
		httpStatus = 403
	case "rate_limit_exceeded":
		httpStatus = 429
	}
	// Redirects are terminal errors, never instructions for the caller to follow.
	if httpStatus < 400 {
		httpStatus = 502
	}
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"type": code, "message": "WorkBuddy upstream request refused [" + code + "]"}})
	if e := sendStart(stream, int32(httpStatus), "application/json"); e != nil {
		return e
	}
	if e := sendBody(stream, raw); e != nil {
		return e
	}
	return sendEnd(stream)
}

func sendStart(s pluginv1.TransportPlugin_ForwardServer, code int32, contentType string) error {
	return s.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: code, ContentLength: -1, Protocol: "HTTP/1.1", ProtocolMajor: 1, ProtocolMinor: 1, Headers: map[string]*pluginv1.HeaderValues{"Content-Type": {Values: []string{contentType}}}}}})
}
func sendBody(s pluginv1.TransportPlugin_ForwardServer, b []byte) error {
	return s.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: append([]byte(nil), b...)}})
}
func sendEnd(s pluginv1.TransportPlugin_ForwardServer) error {
	return s.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{}}})
}
func sendError(s pluginv1.TransportPlugin_ForwardServer, code, message string, sent bool) error {
	return s.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: code, Message: message, RequestSent: sent}}})
}
