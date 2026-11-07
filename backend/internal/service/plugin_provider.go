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
	"github.com/tidwall/gjson"
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
	UnbindPluginAccount(context.Context, int64, int64) error
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
		if current != nil && !current.client.Exited() && current.installation.BinarySHA256 == p.BinarySHA256 {
			if current.installation.ConfigEncrypted == p.ConfigEncrypted {
				continue
			}
			// A login or token rotation on another replica must not kill streams.
			raw, err := m.decryptConfig(p)
			if err == nil {
				applyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				err = current.validateAndApplyConfig(applyCtx, raw)
				cancel()
			}
			if err == nil {
				current.installation.ConfigEncrypted = p.ConfigEncrypted
				continue
			}
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

// Reject stateful features before the compatibility converters can discard them.
func validateProviderIngress(account *Account, body []byte) error {
	if account == nil || account.Type != AccountTypePlugin {
		return nil
	}
	if gjson.GetBytes(body, "background").Bool() || gjson.GetBytes(body, "store").Bool() || gjson.GetBytes(body, "previous_response_id").String() != "" {
		return errors.New("provider plugin supports stateless chat only; background, store and previous_response_id are unavailable")
	}
	return nil
}

func providerUnavailable(message string) error {
	// Prevent an ambiguous request being retried against a different account.
	return &PluginTransportError{Code: "provider_unavailable", Message: message, NoReplay: true}
}

const pluginProviderBaseURL = "https://plugin.invalid"

func (m *PluginManager) providerAccountBinding(ctx context.Context, account *Account) (*PluginAccountBinding, error) {
	if account == nil || account.Platform != PlatformDeepseek || account.Type != AccountTypePlugin {
		return nil, providerUnavailable("invalid plugin account")
	}
	return m.providerAccountBindingByID(ctx, account.ID)
}

func (m *PluginManager) providerAccountBindingByID(ctx context.Context, accountID int64) (*PluginAccountBinding, error) {
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return nil, providerUnavailable("provider repository unavailable")
	}
	binding, err := repo.GetPluginAccountBinding(ctx, accountID)
	if err != nil || binding == nil {
		return nil, providerUnavailable("plugin account is not bound")
	}
	return binding, nil
}

func (m *PluginManager) ProviderAccountModels(ctx context.Context, account *Account) ([]string, error) {
	binding, err := m.providerAccountBinding(ctx, account)
	if err != nil {
		return nil, err
	}
	return m.inspectProviderIdentity(ctx, binding.PluginID, binding.IdentityRef)
}

type PluginAccountCredits struct {
	Remaining      *float64 `json:"remaining"`
	Limit          *float64 `json:"limit,omitempty"`
	CycleResetTime string   `json:"cycle_reset_time,omitempty"`
}

func (m *PluginManager) ProviderAccountCredits(ctx context.Context, accountID int64) (PluginAccountCredits, error) {
	var result PluginAccountCredits
	binding, err := m.providerAccountBindingByID(ctx, accountID)
	if err != nil {
		return result, err
	}
	installation, err := m.repo.GetByID(ctx, binding.PluginID)
	if err != nil || installation.State != PluginStateEnabled || !isProviderPlugin(installation.Manifest) {
		return result, providerUnavailable("provider plugin disabled")
	}
	m.mu.Lock()
	runtime := m.providerRuntimes[binding.PluginID]
	m.mu.Unlock()
	if runtime == nil || runtime.client.Exited() || runtime.installation.BinarySHA256 != installation.BinarySHA256 || !runtime.beginRequest() {
		return result, providerUnavailable("provider plugin unavailable")
	}
	defer runtime.finishRequest()
	raw, err := m.decryptConfig(installation)
	if err != nil {
		return result, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, err := runtime.api.Authorize(queryCtx, &pluginv1.AuthorizeRequest{Action: "credits", IdentityRef: binding.IdentityRef, ConfigJson: raw})
	if err != nil {
		return result, err
	}
	if response.ConfigChanged || len(response.PublicJson) > 4096 || json.Unmarshal(response.PublicJson, &result) != nil ||
		result.Remaining == nil || *result.Remaining < 0 {
		return PluginAccountCredits{}, errors.New("invalid plugin credit response")
	}
	return result, nil
}

func (m *PluginManager) RoundTripProvider(ctx context.Context, req *http.Request, proxy string, account *Account) (*http.Response, error) {
	binding, err := m.providerAccountBinding(ctx, account)
	if err != nil {
		return nil, err
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
	case "begin", "poll", "cancel", "unlink", "inspect", "prepare", "import":
	default:
		return nil, errors.New("unsupported authorization action")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	p, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if action == "import" && !isBuiltinWorkBuddy(p) {
		return nil, errors.New("JSON import is only available for built-in WorkBuddy")
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
	if _, err := m.inspectProviderIdentity(ctx, id, identity); err != nil {
		return err
	}
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return fmt.Errorf("provider repository unavailable")
	}
	return repo.BindPluginAccount(ctx, PluginAccountBinding{AccountID: accountID, PluginID: id, IdentityRef: identity})
}

func (m *PluginManager) inspectProviderIdentity(ctx context.Context, pluginID int64, identity string) ([]string, error) {
	result, err := m.Authorize(ctx, pluginID, "inspect", identity, "")
	if err != nil {
		return nil, err
	}
	var status struct {
		IdentityRef string   `json:"identity_ref"`
		Authorized  bool     `json:"authorized"`
		Models      []string `json:"models"`
	}
	if json.Unmarshal(result, &status) != nil || !status.Authorized || status.IdentityRef != identity {
		return nil, errors.New("identity is not authorized")
	}
	return status.Models, nil
}

func (m *PluginManager) UnbindProviderAccount(ctx context.Context, id, accountID int64) error {
	if id < 1 || accountID < 1 {
		return errors.New("invalid account binding")
	}
	repo, ok := m.repo.(PluginProviderRepository)
	if !ok {
		return errors.New("provider repository unavailable")
	}
	return repo.UnbindPluginAccount(ctx, id, accountID)
}
