package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

// Provider plugins never receive the global account directory or other plugins'
// credentials. Every request must resolve an explicit, unique account binding.
type PluginAccountBinding struct {
	AccountID   int64  `json:"account_id"`
	PluginID    int64  `json:"plugin_id"`
	IdentityRef string `json:"identity_ref"`
}

type PluginProviderRepository interface {
	GetPluginAccountBinding(context.Context, int64) (*PluginAccountBinding, error)
	BindPluginAccount(context.Context, PluginAccountBinding) error
	// Serializes rotating credentials across replicas, not just this process.
	WithPluginConfig(context.Context, int64, string, func(string) (string, error)) error
}

type providerIdentityKey struct{}

func providerIdentityFromContext(ctx context.Context) string {
	v, _ := ctx.Value(providerIdentityKey{}).(string)
	return v
}
func validPluginCapability(c PluginCapability) bool {
	return c.ID == PluginCapabilityOpenAIOAuthOutbound && c.Platform == PlatformOpenAI && c.AccountType == AccountTypeOAuth ||
		c.ID == PluginCapabilityChatCompletions && c.Platform == PlatformDeepseek && c.AccountType == AccountTypePlugin
}
func isProviderPlugin(m PluginManifest) bool {
	for _, c := range m.Capabilities {
		if c.ID == PluginCapabilityChatCompletions {
			return true
		}
	}
	return false
}
func (m *PluginManager) runtimeLocked(id int64) *pluginRuntime {
	if r := m.providerRuntimes[id]; r != nil {
		return r
	}
	return m.runtimes[id]
}

// Called with operationMu held. Provider lifecycle cannot detach legacy OAuth
// runtimes, and a failed provider does not prevent legacy reconciliation.
func (m *PluginManager) reconcileProviders(ctx context.Context, installations []*PluginInstallation) error {
	active := map[int64]bool{}
	for _, p := range installations {
		if !isProviderPlugin(p.Manifest) || p.State != PluginStateEnabled {
			continue
		}
		enabled := false
		for _, b := range p.Bindings {
			if b.Enabled && b.Capability == PluginCapabilityChatCompletions {
				enabled = true
			}
		}
		if !enabled {
			continue
		}
		active[p.ID] = true
		m.mu.Lock()
		current := m.providerRuntimes[p.ID]
		m.mu.Unlock()
		if current != nil && !current.client.Exited() && current.installation.BinarySHA256 == p.BinarySHA256 && current.installation.ConfigEncrypted == p.ConfigEncrypted {
			continue
		}
		if current != nil {
			m.mu.Lock()
			delete(m.providerRuntimes, p.ID)
			m.mu.Unlock()
			current.drain(10 * time.Second)
		}
		local, err := m.ensureLocalInstallation(ctx, p)
		if err != nil {
			continue
		}
		runtime, err := m.prepareRuntime(ctx, local, true)
		if err != nil {
			continue
		}
		latest, err := m.repo.GetByID(ctx, p.ID)
		if err != nil || latest.State != PluginStateEnabled || latest.BinarySHA256 != p.BinarySHA256 || latest.ConfigEncrypted != p.ConfigEncrypted {
			runtime.kill()
			continue
		}
		m.mu.Lock()
		if m.providerRuntimes == nil {
			m.providerRuntimes = make(map[int64]*pluginRuntime)
		}
		m.providerRuntimes[p.ID] = runtime
		m.mu.Unlock()
	}
	m.mu.Lock()
	stale := []*pluginRuntime{}
	for id, r := range m.providerRuntimes {
		if !active[id] {
			stale = append(stale, r)
			delete(m.providerRuntimes, id)
		}
	}
	m.mu.Unlock()
	for _, r := range stale {
		r.drain(10 * time.Second)
	}
	return nil
}

func providerUnavailable(message string) error {
	// Prevent an ambiguous request being retried against a different account.
	return &PluginTransportError{Code: "provider_unavailable", Message: message, NoReplay: true}
}

func (m *PluginManager) RoundTripProvider(ctx context.Context, req *http.Request, proxy string, account *Account) (*http.Response, error) {
	if account == nil || account.Platform != PlatformDeepseek || account.Type != AccountTypePlugin {
		return nil, providerUnavailable("invalid plugin account")
	}
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return nil, providerUnavailable("provider repository unavailable")
	}
	binding, err := repo.GetPluginAccountBinding(ctx, account.ID)
	if err != nil {
		return nil, providerUnavailable("plugin account is not bound")
	}
	// Refresh uses the same encrypted, transactionally locked config as admin auth.
	if _, err = m.Authorize(ctx, binding.PluginID, "prepare", binding.IdentityRef, ""); err != nil {
		return nil, providerUnavailable("plugin identity preparation failed")
	}
	p, err := m.repo.GetByID(ctx, binding.PluginID)
	if err != nil || p.State != PluginStateEnabled {
		return nil, providerUnavailable("provider plugin disabled")
	}
	m.mu.Lock()
	runtime := m.providerRuntimes[binding.PluginID]
	m.mu.Unlock()
	if runtime == nil || runtime.client.Exited() || runtime.installation.BinarySHA256 != p.BinarySHA256 || !runtime.beginRequest() {
		return nil, providerUnavailable("provider plugin unavailable")
	}
	// The plugin endpoint is virtual. Strip client and host credentials before RPC.
	clean := req.Clone(ctx)
	clean.Header = make(http.Header)
	clean.Header.Set("Content-Type", "application/json")
	clean.Header.Set("Accept", req.Header.Get("Accept"))
	response, err := runtime.roundTrip(context.WithValue(ctx, providerIdentityKey{}, binding.IdentityRef), clean, proxy, account)
	if err != nil {
		runtime.finishRequest()
		var transportErr *PluginTransportError
		if errors.As(err, &transportErr) {
			transportErr.NoReplay = true
		}
		return nil, err
	}
	return response, nil
}

// Authorize is an optional, additive RPC. The public result is a strict JSON
// document supplied by trusted plugin code; private config never enters HTTP.
func (m *PluginManager) Authorize(ctx context.Context, id int64, action, identity, session string) (json.RawMessage, error) {
	switch action {
	case "begin", "poll", "cancel", "unlink", "inspect", "prepare":
	default:
		return nil, errors.New("unsupported authorization action")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	p, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isProviderPlugin(p.Manifest) || p.State != PluginStateEnabled {
		return nil, errors.New("enable the provider plugin before authorization")
	}
	m.mu.Lock()
	runtime := m.providerRuntimes[id]
	m.mu.Unlock()
	if runtime == nil || runtime.client.Exited() || !runtime.beginRequest() {
		return nil, errors.New("provider plugin unavailable")
	}
	defer runtime.finishRequest()
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return nil, errors.New("provider repository unavailable")
	}
	var result json.RawMessage
	err = repo.WithPluginConfig(ctx, id, p.BinarySHA256, func(encrypted string) (string, error) {
		current := *p
		current.ConfigEncrypted = encrypted
		raw, e := m.decryptConfig(&current)
		if e != nil {
			return "", e
		}
		callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		answer, e := runtime.api.Authorize(callCtx, &pluginv1.AuthorizeRequest{Action: action, IdentityRef: identity, SessionId: session, ConfigJson: raw})
		if e != nil {
			return "", errors.New("plugin authorization failed; inspect sanitized plugin status")
		}
		if len(answer.PublicJson) > 64*1024 || !json.Valid(answer.PublicJson) {
			return "", errors.New("invalid plugin authorization result")
		}
		result = append(json.RawMessage(nil), answer.PublicJson...)
		if !answer.ConfigChanged {
			return encrypted, nil
		}
		if len(answer.ConfigJson) > pluginConfigMaxBytes || !json.Valid(answer.ConfigJson) {
			return "", errors.New("invalid private authorization config")
		}
		normalized, e := runtime.validateAndApplyNormalizedConfig(callCtx, answer.ConfigJson)
		if e != nil {
			return "", errors.New("invalid authorization configuration")
		}
		next, e := m.encryptor.Encrypt(string(normalized))
		if e != nil {
			return "", e
		}
		return next, nil
	})
	// Always recover the authoritative DB config after an RPC/commit failure.
	latest, getErr := m.repo.GetByID(ctx, id)
	if getErr == nil {
		raw, e := m.decryptConfig(latest)
		if e == nil {
			e = runtime.validateAndApplyConfig(ctx, raw)
		}
		if e == nil {
			runtime.installation.ConfigEncrypted = latest.ConfigEncrypted
		} else {
			runtime.kill()
			err = errors.Join(err, e)
		}
	} else {
		runtime.kill()
		err = errors.Join(err, getErr)
	}
	return result, err
}

func (m *PluginManager) BindProviderAccount(ctx context.Context, id, accountID int64, identity string) error {
	identity = strings.TrimSpace(identity)
	if accountID < 1 || identity == "" || len(identity) > 160 {
		return errors.New("invalid account binding")
	}
	result, err := m.Authorize(ctx, id, "inspect", identity, "")
	if err != nil {
		return err
	}
	var status struct {
		IdentityRef string `json:"identity_ref"`
		Authorized  bool   `json:"authorized"`
	}
	if json.Unmarshal(result, &status) != nil || !status.Authorized || status.IdentityRef != identity {
		return errors.New("identity is not authorized")
	}
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return fmt.Errorf("provider repository unavailable")
	}
	return repo.BindPluginAccount(ctx, PluginAccountBinding{AccountID: accountID, PluginID: id, IdentityRef: identity})
}
