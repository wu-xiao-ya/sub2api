package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

const (
	defaultTrafficRelayUnavailableTTL = 45 * time.Second
	minTrafficRelayUnavailableTTL     = 5 * time.Second
	maxTrafficRelayUnavailableTTL     = 10 * time.Minute
	trafficRelayDownRedisKey          = "traffic_relay:down"
)

// AccountOutbound is the resolved upstream proxy for one account request.
type AccountOutbound struct {
	ProxyURL         string
	ViaRelay         bool
	FallbackProxyURL string
	RelayHost        string
	unavailableTTL   time.Duration
}

type relayRouteFailure struct{ err error }

func (e *relayRouteFailure) Error() string { return e.err.Error() }
func (e *relayRouteFailure) Unwrap() error { return e.err }

func isRelayRouteFailure(err error) bool {
	var failure *relayRouteFailure
	return errors.As(err, &failure)
}

func markRelayRouteFailure(err error, route AccountOutbound) error {
	if err != nil && route.ViaRelay && IsRelayConnectError(err, route.RelayHost) && !isRelayRouteFailure(err) {
		return &relayRouteFailure{err: err}
	}
	return err
}

type accountOutboundContextKey struct{}

type requestAccountOutbound struct {
	accountID int64
	outbound  AccountOutbound
}

type cachedRelaySnapshot struct {
	url     string
	host    string
	ttl     time.Duration
	expires time.Time
}

// AccountOutboundService owns relay configuration and shared outage state.
// Individual request routes live exclusively in their request contexts.
type AccountOutboundService struct {
	settings  *SettingService
	proxyRepo ProxyRepository
	rdb       *redis.Client

	mu        sync.Mutex
	downUntil map[string]time.Time
	relaySnap cachedRelaySnapshot
}

var processAccountOutbound atomic.Pointer[AccountOutboundService]

// NewAccountOutboundService constructs the process-wide traffic relay resolver.
func NewAccountOutboundService(settings *SettingService, proxyRepo ProxyRepository, rdb *redis.Client) *AccountOutboundService {
	svc := &AccountOutboundService{
		settings:  settings,
		proxyRepo: proxyRepo,
		rdb:       rdb,
	}
	SetProcessAccountOutbound(svc)
	return svc
}

// SetProcessAccountOutbound installs the resolver used by ResolveAccountProxyURL.
func SetProcessAccountOutbound(svc *AccountOutboundService) {
	processAccountOutbound.Store(svc)
}

func processOutbound() *AccountOutboundService {
	return processAccountOutbound.Load()
}

// AccountDefaultProxyURL returns the account's own proxy, or empty for direct.
func AccountDefaultProxyURL(account *Account) string {
	if account == nil || account.ProxyID == nil || account.Proxy == nil {
		return ""
	}
	return strings.TrimSpace(account.Proxy.URL())
}

// ResolveAccountProxyURL returns the proxy URL this account should use now.
func ResolveAccountProxyURL(account *Account) string {
	return resolveAccountOutbound(account).ProxyURL
}

// WithAccountOutbound binds an immutable route to this attempt, never to a
// shared account slot. Explicit per-call proxy overrides remain unchanged.
func WithAccountOutbound(req *http.Request, account *Account, proxyURL string) *http.Request {
	if req == nil || account == nil {
		return req
	}
	outbound := resolveRequestedAccountOutbound(req.Context(), account, proxyURL)
	ctx := req.Context()
	if outbound.ViaRelay {
		ctx = proxyutil.WithRelayConnectHost(ctx, outbound.RelayHost)
	}
	return req.WithContext(context.WithValue(ctx, accountOutboundContextKey{},
		requestAccountOutbound{accountID: account.ID, outbound: outbound}))
}

func requestOutbound(req *http.Request, accountID int64) (AccountOutbound, bool) {
	if req == nil {
		return AccountOutbound{}, false
	}
	value, ok := req.Context().Value(accountOutboundContextKey{}).(requestAccountOutbound)
	return value.outbound, ok && value.accountID == accountID
}

func resolveAccountOutbound(account *Account) AccountOutbound {
	fallback := AccountDefaultProxyURL(account)
	if account == nil || !account.UseRelayRoute {
		return AccountOutbound{ProxyURL: fallback}
	}
	svc := processOutbound()
	if svc == nil {
		return AccountOutbound{ProxyURL: fallback}
	}
	return svc.Resolve(context.Background(), account)
}

// Resolve returns the current outbound for an account.
func (s *AccountOutboundService) Resolve(ctx context.Context, account *Account) AccountOutbound {
	fallback := AccountDefaultProxyURL(account)
	if s == nil || account == nil || !account.UseRelayRoute {
		return AccountOutbound{ProxyURL: fallback}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	snap := s.relaySnapshot(ctx)
	if snap.url == "" || s.isDown(ctx, snap.url) {
		return AccountOutbound{ProxyURL: fallback}
	}
	return AccountOutbound{
		ProxyURL:         snap.url,
		ViaRelay:         true,
		FallbackProxyURL: fallback,
		RelayHost:        snap.host,
		unavailableTTL:   snap.ttl,
	}
}

func (s *AccountOutboundService) relaySnapshot(ctx context.Context) cachedRelaySnapshot {
	if s == nil {
		return cachedRelaySnapshot{ttl: defaultTrafficRelayUnavailableTTL}
	}
	now := time.Now()
	s.mu.Lock()
	if now.Before(s.relaySnap.expires) {
		snap := s.relaySnap
		s.mu.Unlock()
		return snap
	}
	s.mu.Unlock()

	snap := cachedRelaySnapshot{ttl: defaultTrafficRelayUnavailableTTL}
	if s.settings != nil {
		if settings, err := s.settings.GetAllSettings(ctx); err == nil && settings != nil {
			if settings.TrafficRelayUnavailableTTLSeconds > 0 {
				snap.ttl = time.Duration(settings.TrafficRelayUnavailableTTLSeconds) * time.Second
			}
			if settings.TrafficRelayEnabled && settings.TrafficRelayProxyID > 0 && s.proxyRepo != nil {
				if proxy, err := s.proxyRepo.GetByID(ctx, settings.TrafficRelayProxyID); err == nil && proxy != nil && proxy.IsActive() {
					raw := strings.TrimSpace(proxy.URL())
					if parsed, err := url.Parse(raw); err == nil && parsed.Scheme == "http" && parsed.Host != "" {
						snap.url = raw
						snap.host = parsed.Host
					}
				}
			}
		}
	}
	if snap.ttl < minTrafficRelayUnavailableTTL {
		snap.ttl = minTrafficRelayUnavailableTTL
	}
	if snap.ttl > maxTrafficRelayUnavailableTTL {
		snap.ttl = maxTrafficRelayUnavailableTTL
	}
	snap.expires = now.Add(5 * time.Second)
	s.mu.Lock()
	s.relaySnap = snap
	s.mu.Unlock()
	return snap
}

func (s *AccountOutboundService) relayURL(ctx context.Context) (string, string) {
	snap := s.relaySnapshot(ctx)
	return snap.url, snap.host
}

func relayDownKey(proxyURL string) string {
	return fmt.Sprintf("%s:%x", trafficRelayDownRedisKey, sha256.Sum256([]byte(proxyURL)))
}

func (s *AccountOutboundService) isDown(ctx context.Context, proxyURL string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	key := relayDownKey(proxyURL)
	until := s.downUntil[key]
	s.mu.Unlock()
	if !until.IsZero() && time.Now().Before(until) {
		return true
	}
	if s.rdb == nil {
		return false
	}
	n, err := s.rdb.Exists(ctx, key).Result()
	return err == nil && n > 0
}

// MarkRelayDown remembers that the system relay is temporarily unavailable.
func MarkRelayDown(ctx context.Context, route AccountOutbound) {
	if svc := processOutbound(); svc != nil {
		svc.MarkDown(ctx, route)
	}
}

func (s *AccountOutboundService) MarkDown(ctx context.Context, route AccountOutbound) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ttl := route.unavailableTTL
	if ttl <= 0 {
		ttl = defaultTrafficRelayUnavailableTTL
	}
	key := relayDownKey(route.ProxyURL)
	now := time.Now()
	until := now.Add(ttl)
	s.mu.Lock()
	if s.downUntil == nil {
		s.downUntil = make(map[string]time.Time)
	}
	for oldKey, deadline := range s.downUntil {
		if !now.Before(deadline) {
			delete(s.downUntil, oldKey)
		}
	}
	if until.After(s.downUntil[key]) {
		s.downUntil[key] = until
	}
	s.mu.Unlock()
	if s.rdb != nil {
		// Delayed failures must not shorten a newer outage's remaining lifetime.
		const extendOutage = `
local current = redis.call('PTTL', KEYS[1])
local requested = tonumber(ARGV[1])
if current < requested then
  redis.call('PSETEX', KEYS[1], requested, '1')
end
return 1`
		if err := s.rdb.Eval(ctx, extendOutage, []string{key}, ttl.Milliseconds()).Err(); err != nil {
			logger.L().Warn("traffic_relay.mark_down_redis_failed", zap.Error(err))
		}
	}
}

func cloneHTTPRequest(req *http.Request) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}
	clone := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("request body cannot be replayed")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

// IsRelayConnectError reports whether err happened while connecting to the relay.
func IsRelayConnectError(err error, relayHost string) bool {
	if err == nil {
		return false
	}
	var connectErr *proxyutil.ConnectError
	if errors.As(err, &connectErr) {
		if connectErr.ProxyHost != relayHost {
			return false
		}
		return connectErr.StatusCode == 0 || connectErr.StatusCode == http.StatusProxyAuthRequired ||
			connectErr.StatusCode == http.StatusForbidden
	}
	msg := strings.ToLower(err.Error())
	// A CONNECT 502/503/504 can mean the proxy cannot reach the origin.
	// Do not classify every proxyconnect error as a relay outage.
	if strings.Contains(msg, "proxy authentication required") {
		return true
	}
	if strings.Contains(msg, "proxyconnect") {
		return strings.Contains(msg, "i/o timeout") ||
			strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "no such host") ||
			strings.Contains(msg, "network is unreachable") ||
			strings.Contains(msg, "eof") ||
			strings.Contains(msg, "forbidden")
	}
	host := relayHostname(relayHost)
	if host == "" || !strings.Contains(msg, host) {
		return false
	}
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout awaiting") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection timed out")
}

func relayHostname(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(host, "[]")
}

// RetryRelayHTTP retries a failed relay CONNECT on the account default route.
func RetryRelayHTTP(ctx context.Context, accountID int64, proxyURL string, req *http.Request, err error, do func(*http.Request, string) (*http.Response, error)) (*http.Response, error, bool) {
	if err == nil || do == nil {
		return nil, err, false
	}
	if (ctx != nil && ctx.Err() != nil) || (req != nil && req.Context().Err() != nil) {
		return nil, err, false
	}
	outbound, ok := requestOutbound(req, accountID)
	if !ok || !outbound.ViaRelay || outbound.ProxyURL != proxyURL || !IsRelayConnectError(err, outbound.RelayHost) {
		return nil, err, false
	}
	MarkRelayDown(ctx, outbound)
	if outbound.FallbackProxyURL == outbound.ProxyURL {
		return nil, err, false
	}
	clone, cloneErr := cloneHTTPRequest(req)
	if cloneErr != nil {
		return nil, err, false
	}
	logger.L().Warn("traffic_relay.fallback_to_default",
		zap.Int64("account_id", accountID),
		zap.String("relay_host", outbound.RelayHost),
		zap.Error(err),
	)
	resp, retryErr := do(clone, outbound.FallbackProxyURL)
	return resp, retryErr, true
}
