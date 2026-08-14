package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrContributionDuplicate = infraerrors.Conflict("CONTRIBUTION_DUPLICATE", "openai account has already been contributed")
var ErrContributionInvalidStatus = infraerrors.Conflict("CONTRIBUTION_INVALID_STATUS", "invalid contribution status")
var ErrContributionOwnership = infraerrors.NotFound("CONTRIBUTION_NOT_FOUND", "account contribution not found")

type AccountContributionService struct {
	accountRepo        AccountContributionRepository
	groupRepo          GroupRepository
	proxyRepo          ProxyRepository
	rewardRepo         ContributorRewardRepository
	oauth              *OpenAIOAuthService
	schedulerRefresher SchedulerAccountRefresher
}

type AccountContributionRepository interface {
	Create(ctx context.Context, account *Account) error
	GetByID(ctx context.Context, id int64) (*Account, error)
	Update(ctx context.Context, account *Account) error
	BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error
	FindByExtraField(ctx context.Context, key string, value any) ([]Account, error)
	ListContributionsByOwner(ctx context.Context, ownerUserID int64, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error)
	ListContributionsByStatus(ctx context.Context, status string, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error)
}

type SchedulerAccountRefresher interface {
	RefreshAccount(ctx context.Context, account *Account, groupIDs []int64, reason string) error
}

func NewAccountContributionService(accountRepo AccountContributionRepository, groupRepo GroupRepository, rewardRepo ContributorRewardRepository, oauth *OpenAIOAuthService) *AccountContributionService {
	return &AccountContributionService{accountRepo: accountRepo, groupRepo: groupRepo, rewardRepo: rewardRepo, oauth: oauth}
}

func ProvideAccountContributionService(accountRepo AccountContributionRepository, groupRepo GroupRepository, proxyRepo ProxyRepository, rewardRepo ContributorRewardRepository, oauth *OpenAIOAuthService, schedulerSnapshot *SchedulerSnapshotService) *AccountContributionService {
	svc := NewAccountContributionService(accountRepo, groupRepo, rewardRepo, oauth)
	svc.proxyRepo = proxyRepo
	svc.SetSchedulerRefresher(schedulerSnapshot)
	return svc
}

func (s *AccountContributionService) SetSchedulerRefresher(refresher SchedulerAccountRefresher) {
	if s == nil {
		return
	}
	s.schedulerRefresher = refresher
}

type SubmitOpenAIContributionInput struct {
	SessionID   string
	Code        string
	State       string
	RedirectURI string
	ProxyID     *int64
	ProxyURL    string
	Name        string
}

type SubmitOpenAIJSONContributionInput struct {
	Accounts []OpenAIJSONContributionAccount
	Proxies  []OpenAIJSONContributionProxy
	ProxyID  *int64
	ProxyURL string
}

type OpenAIJSONContributionProxy struct {
	ProxyKey string
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
	Status   string
}

type OpenAIJSONContributionAccount struct {
	Name               string
	Notes              *string
	Platform           string
	Type               string
	Credentials        map[string]any
	Extra              map[string]any
	ProxyKey           *string
	Concurrency        int
	Priority           int
	ExpiresAt          *int64
	AutoPauseOnExpired *bool
}

type ContributionImportResult struct {
	Total   int                       `json:"total"`
	Created int                       `json:"created"`
	Failed  int                       `json:"failed"`
	Items   []ContributionImportItem  `json:"items,omitempty"`
	Errors  []ContributionImportError `json:"errors,omitempty"`
}

type ContributionImportPreview struct {
	Total       int                             `json:"total"`
	Valid       int                             `json:"valid"`
	Duplicate   int                             `json:"duplicate"`
	Unsupported int                             `json:"unsupported"`
	Invalid     int                             `json:"invalid"`
	Items       []ContributionImportPreviewItem `json:"items,omitempty"`
}

type ContributionImportPreviewItem struct {
	Index           int    `json:"index"`
	Name            string `json:"name,omitempty"`
	Valid           bool   `json:"valid"`
	Duplicate       bool   `json:"duplicate"`
	Unsupported     bool   `json:"unsupported"`
	Invalid         bool   `json:"invalid"`
	IdentityPresent bool   `json:"identity_present"`
	Message         string `json:"message,omitempty"`
}

type ContributionImportItem struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	AccountID int64  `json:"account_id,omitempty"`
	Action    string `json:"action"`
	Message   string `json:"message,omitempty"`
}

type ContributionImportError struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
}

type ApproveContributionInput struct {
	GroupIDs    []int64
	Concurrency *int
	Priority    *int
}

type ContributionAccountConfigInput struct {
	Name                     *string
	Notes                    *string
	Concurrency              *int
	LoadFactor               *int
	ExpiresAt                *int64
	AutoPauseOnExpired       *bool
	TempUnschedulableEnabled *bool
	TempUnschedulableRules   *[]TempUnschedulableRule
	AutoPause5hThreshold     *float64
	AutoPause7dThreshold     *float64
	AutoPause5hDisabled      *bool
	AutoPause7dDisabled      *bool
}

func (s *AccountContributionService) GenerateOpenAIAuthURL(ctx context.Context, _ int64, proxyID *int64, redirectURI string) (*OpenAIAuthURLResult, error) {
	return s.oauth.GenerateAuthURL(ctx, proxyID, redirectURI, PlatformOpenAI)
}

func (s *AccountContributionService) SubmitOpenAI(ctx context.Context, userID int64, input SubmitOpenAIContributionInput) (*Account, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	proxyID := input.ProxyID
	ownedProxyID, err := s.createContributionOwnedProxy(ctx, userID, "OpenAI OAuth Contribution", input.ProxyURL)
	if err != nil {
		return nil, err
	}
	if ownedProxyID != nil {
		proxyID = ownedProxyID
		defer func() {
			if err != nil {
				_ = s.deleteContributionOwnedProxy(ctx, ownedProxyID)
			}
		}()
	}
	tokenInfo, err := s.oauth.ExchangeCode(ctx, &OpenAIExchangeCodeInput{
		SessionID:   input.SessionID,
		Code:        input.Code,
		State:       input.State,
		RedirectURI: input.RedirectURI,
		ProxyID:     proxyID,
	})
	if err != nil {
		return nil, err
	}
	credentials := s.oauth.BuildAccountCredentials(tokenInfo)
	identityKey := contributionIdentityKey(credentials)
	if identityKey == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_IDENTITY_MISSING", "cannot determine contributed OpenAI account identity")
	}
	extra := map[string]any{
		"contribution_identity_key": identityKey,
		"contribution_source":       "user_openai_oauth",
	}
	markContributionOwnedProxyExtra(extra, ownedProxyID)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		for _, candidate := range []string{tokenInfo.Email, tokenInfo.ChatGPTAccountID, tokenInfo.ChatGPTUserID} {
			if strings.TrimSpace(candidate) != "" {
				name = strings.TrimSpace(candidate)
				break
			}
		}
	}
	if name == "" {
		name = "OpenAI OAuth Contribution"
	}
	account, err := s.createPendingOpenAIContribution(ctx, userID, pendingOpenAIContributionInput{
		Name:                    name,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		Credentials:             credentials,
		Extra:                   extra,
		ProxyID:                 proxyID,
		Concurrency:             1,
		Priority:                100,
		AutoPauseOnExpired:      true,
		ContributionIdentityKey: identityKey,
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (s *AccountContributionService) PreviewOpenAIJSON(ctx context.Context, userID int64, input SubmitOpenAIJSONContributionInput) (ContributionImportPreview, error) {
	preview := ContributionImportPreview{Total: len(input.Accounts)}
	if userID <= 0 {
		return preview, ErrUserNotFound
	}
	if len(input.Accounts) == 0 {
		return preview, infraerrors.BadRequest("CONTRIBUTION_IMPORT_EMPTY", "accounts is required")
	}
	if len(input.Accounts) > 100 {
		return preview, infraerrors.BadRequest("CONTRIBUTION_IMPORT_TOO_MANY", "too many accounts in one import")
	}
	if strings.TrimSpace(input.ProxyURL) != "" {
		if _, err := parseContributionProxyURL(input.ProxyURL); err != nil {
			return preview, err
		}
	}
	proxyByKey, err := buildOpenAIContributionProxyMap(input.Proxies)
	if err != nil {
		return preview, err
	}

	seenInPayload := make(map[string]int, len(input.Accounts))
	for i := range input.Accounts {
		item := input.Accounts[i]
		name := strings.TrimSpace(item.Name)
		previewItem := ContributionImportPreviewItem{Index: i, Name: name}

		if err := validateOpenAIJSONContributionAccount(item); err != nil {
			previewItem.Message = err.Error()
			if isContributionUnsupportedError(err) {
				previewItem.Unsupported = true
				preview.Unsupported++
			} else {
				previewItem.Invalid = true
				preview.Invalid++
			}
			preview.Items = append(preview.Items, previewItem)
			continue
		}
		if input.ProxyID == nil && strings.TrimSpace(input.ProxyURL) == "" {
			proxyKey := contributionAccountProxyKey(item)
			if proxyKey != "" {
				if _, ok := proxyByKey[proxyKey]; !ok {
					previewItem.Invalid = true
					previewItem.Message = "proxy_key not found"
					preview.Invalid++
					preview.Items = append(preview.Items, previewItem)
					continue
				}
			}
		}

		enrichOpenAIContributionCredentialsFromIDToken(item.Credentials)
		identityKey := contributionIdentityKey(item.Credentials)
		if identityKey == "" {
			previewItem.Invalid = true
			previewItem.Message = "cannot determine contributed OpenAI account identity"
			preview.Invalid++
			preview.Items = append(preview.Items, previewItem)
			continue
		}
		previewItem.IdentityPresent = true

		if firstIndex, ok := seenInPayload[identityKey]; ok {
			previewItem.Duplicate = true
			previewItem.Message = fmt.Sprintf("duplicate with item #%d in current import", firstIndex+1)
			preview.Duplicate++
			preview.Items = append(preview.Items, previewItem)
			continue
		}
		seenInPayload[identityKey] = i

		dups, err := s.accountRepo.FindByExtraField(ctx, "contribution_identity_key", identityKey)
		if err != nil {
			return preview, err
		}
		for j := range dups {
			if dups[j].OwnerUserID != nil && dups[j].ContributionStatus != "" {
				previewItem.Duplicate = true
				previewItem.Message = "openai account has already been contributed"
				break
			}
		}
		if previewItem.Duplicate {
			preview.Duplicate++
		} else {
			previewItem.Valid = true
			preview.Valid++
		}
		preview.Items = append(preview.Items, previewItem)
	}

	return preview, nil
}

func (s *AccountContributionService) SubmitOpenAIJSON(ctx context.Context, userID int64, input SubmitOpenAIJSONContributionInput) (ContributionImportResult, error) {
	result := ContributionImportResult{Total: len(input.Accounts)}
	if userID <= 0 {
		return result, ErrUserNotFound
	}
	if len(input.Accounts) == 0 {
		return result, infraerrors.BadRequest("CONTRIBUTION_IMPORT_EMPTY", "accounts is required")
	}
	if len(input.Accounts) > 100 {
		return result, infraerrors.BadRequest("CONTRIBUTION_IMPORT_TOO_MANY", "too many accounts in one import")
	}
	proxyByKey, err := buildOpenAIContributionProxyMap(input.Proxies)
	if err != nil {
		return result, err
	}

	for i := range input.Accounts {
		item := input.Accounts[i]
		name := strings.TrimSpace(item.Name)
		if err := validateOpenAIJSONContributionAccount(item); err != nil {
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		enrichOpenAIContributionCredentialsFromIDToken(item.Credentials)
		identityKey := contributionIdentityKey(item.Credentials)
		if identityKey == "" {
			err := infraerrors.BadRequest("CONTRIBUTION_IDENTITY_MISSING", "cannot determine contributed OpenAI account identity")
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		extra := sanitizedContributionExtra(item.Extra)
		extra["contribution_identity_key"] = identityKey
		extra["contribution_source"] = "user_openai_json"
		proxyID := input.ProxyID
		ownedProxyID, err := s.createContributionOwnedProxy(ctx, userID, name, input.ProxyURL)
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}
		if ownedProxyID != nil {
			proxyID = ownedProxyID
		}
		if proxyID == nil {
			proxyKey := contributionAccountProxyKey(item)
			if proxyKey != "" {
				importedProxy, ok := proxyByKey[proxyKey]
				if !ok {
					err := infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_KEY_NOT_FOUND", "proxy_key not found")
					result.Failed++
					result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
					result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
					continue
				}
				ownedProxyID, err = s.createContributionOwnedProxyFromImport(ctx, userID, name, importedProxy)
				if err != nil {
					result.Failed++
					result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
					result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
					continue
				}
				proxyID = ownedProxyID
			}
		}
		markContributionOwnedProxyExtra(extra, ownedProxyID)

		if name == "" {
			for _, candidate := range []string{
				strings.TrimSpace(fmt.Sprint(item.Credentials["email"])),
				strings.TrimSpace(fmt.Sprint(item.Credentials["chatgpt_account_id"])),
				strings.TrimSpace(fmt.Sprint(item.Credentials["chatgpt_user_id"])),
			} {
				if candidate != "" && candidate != "<nil>" {
					name = candidate
					break
				}
			}
		}
		if name == "" {
			name = "OpenAI JSON Contribution"
		}
		concurrency := item.Concurrency
		if concurrency <= 0 {
			concurrency = 1
		}
		priority := item.Priority
		if priority <= 0 {
			priority = 100
		}
		autoPause := true
		if item.AutoPauseOnExpired != nil {
			autoPause = *item.AutoPauseOnExpired
		}
		var expiresAt *time.Time
		if item.ExpiresAt != nil && *item.ExpiresAt > 0 {
			v := time.Unix(*item.ExpiresAt, 0).UTC()
			expiresAt = &v
		}

		account, err := s.createPendingOpenAIContribution(ctx, userID, pendingOpenAIContributionInput{
			Name:                    name,
			Notes:                   item.Notes,
			Platform:                PlatformOpenAI,
			Type:                    AccountTypeOAuth,
			Credentials:             item.Credentials,
			Extra:                   extra,
			ProxyID:                 proxyID,
			Concurrency:             concurrency,
			Priority:                priority,
			ExpiresAt:               expiresAt,
			AutoPauseOnExpired:      autoPause,
			ContributionIdentityKey: identityKey,
		})
		if err != nil {
			_ = s.deleteContributionOwnedProxy(ctx, ownedProxyID)
			result.Failed++
			result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, Action: "failed", Message: err.Error()})
			result.Errors = append(result.Errors, ContributionImportError{Index: i, Name: name, Message: err.Error()})
			continue
		}

		result.Created++
		result.Items = append(result.Items, ContributionImportItem{Index: i, Name: name, AccountID: account.ID, Action: "created"})
	}

	return result, nil
}

type pendingOpenAIContributionInput struct {
	Name                    string
	Notes                   *string
	Platform                string
	Type                    string
	Credentials             map[string]any
	Extra                   map[string]any
	ProxyID                 *int64
	Concurrency             int
	Priority                int
	ExpiresAt               *time.Time
	AutoPauseOnExpired      bool
	ContributionIdentityKey string
}

func (s *AccountContributionService) createPendingOpenAIContribution(ctx context.Context, userID int64, input pendingOpenAIContributionInput) (*Account, error) {
	identityKey := strings.TrimSpace(input.ContributionIdentityKey)
	if identityKey == "" {
		identityKey = contributionIdentityKey(input.Credentials)
	}
	if identityKey == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_IDENTITY_MISSING", "cannot determine contributed OpenAI account identity")
	}
	dups, err := s.accountRepo.FindByExtraField(ctx, "contribution_identity_key", identityKey)
	if err != nil {
		return nil, err
	}
	for i := range dups {
		if dups[i].OwnerUserID != nil && dups[i].ContributionStatus != "" {
			return nil, ErrContributionDuplicate
		}
	}
	extra := input.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	extra["contribution_identity_key"] = identityKey
	now := time.Now()
	account := &Account{
		Name:                    input.Name,
		Notes:                   input.Notes,
		Platform:                input.Platform,
		Type:                    input.Type,
		Credentials:             input.Credentials,
		Extra:                   extra,
		ProxyID:                 input.ProxyID,
		Concurrency:             input.Concurrency,
		Priority:                input.Priority,
		Status:                  StatusDisabled,
		Schedulable:             false,
		OwnerUserID:             &userID,
		ContributionStatus:      ContributionStatusPending,
		ContributionSubmittedAt: &now,
		ExpiresAt:               input.ExpiresAt,
		AutoPauseOnExpired:      input.AutoPauseOnExpired,
	}
	if err := s.accountRepo.Create(ctx, account); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_accounts_contribution_identity") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, ErrContributionDuplicate
		}
		return nil, err
	}
	return account, nil
}

func validateOpenAIJSONContributionAccount(item OpenAIJSONContributionAccount) error {
	if strings.TrimSpace(item.Platform) != "" && strings.ToLower(strings.TrimSpace(item.Platform)) != PlatformOpenAI {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PLATFORM_UNSUPPORTED", "only openai account JSON is supported")
	}
	if strings.TrimSpace(item.Type) != "" && strings.ToLower(strings.TrimSpace(item.Type)) != AccountTypeOAuth {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_TYPE_UNSUPPORTED", "only openai oauth account JSON is supported")
	}
	if len(item.Credentials) == 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_CREDENTIALS_REQUIRED", "account credentials is required")
	}
	if strings.TrimSpace(fmt.Sprint(item.Credentials["access_token"])) == "" || strings.TrimSpace(fmt.Sprint(item.Credentials["access_token"])) == "<nil>" {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_ACCESS_TOKEN_REQUIRED", "openai oauth credentials require access_token")
	}
	if item.Concurrency < 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_INVALID_CONCURRENCY", "concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_INVALID_PRIORITY", "priority must be >= 0")
	}
	return nil
}

func isContributionUnsupportedError(err error) bool {
	reason := infraerrors.Reason(err)
	return reason == "CONTRIBUTION_IMPORT_PLATFORM_UNSUPPORTED" || reason == "CONTRIBUTION_IMPORT_TYPE_UNSUPPORTED"
}

func sanitizedContributionExtra(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+2)
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if strings.HasPrefix(normalized, "contribution_") {
			continue
		}
		out[key] = value
	}
	return out
}

func enrichOpenAIContributionCredentialsFromIDToken(credentials map[string]any) {
	if credentials == nil {
		return
	}
	idToken, _ := credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		return
	}
	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}
	setIfMissing := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if existing, _ := credentials[key].(string); strings.TrimSpace(existing) == "" {
			credentials[key] = value
		}
	}
	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
}

func (s *AccountContributionService) ListMine(ctx context.Context, userID int64, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return s.accountRepo.ListContributionsByOwner(ctx, userID, params)
}

func (s *AccountContributionService) GetMine(ctx context.Context, userID, accountID int64) (*Account, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != userID || account.ContributionStatus == "" {
		return nil, ErrContributionOwnership
	}
	return account, nil
}

func (s *AccountContributionService) UpdateMineConfig(ctx context.Context, userID, accountID int64, input ContributionAccountConfigInput) (*Account, error) {
	account, err := s.GetMine(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if account.ContributionStatus != ContributionStatusPending && account.ContributionStatus != ContributionStatusApproved {
		return nil, ErrContributionInvalidStatus
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, infraerrors.BadRequest("CONTRIBUTION_NAME_REQUIRED", "name is required")
		}
		account.Name = name
	}
	if input.Notes != nil {
		account.Notes = normalizeAccountNotes(input.Notes)
	}
	if input.Concurrency != nil {
		if *input.Concurrency < 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_INVALID_CONCURRENCY", "concurrency must be >= 0")
		}
		account.Concurrency = normalizeAccountConcurrency(account.Platform, account.Type, *input.Concurrency)
	}
	if input.LoadFactor != nil {
		if *input.LoadFactor <= 0 {
			account.LoadFactor = nil
		} else if *input.LoadFactor > 10000 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_INVALID_LOAD_FACTOR", "load_factor must be <= 10000")
		} else {
			loadFactor := *input.LoadFactor
			account.LoadFactor = &loadFactor
		}
	}
	if input.ExpiresAt != nil {
		if *input.ExpiresAt <= 0 {
			account.ExpiresAt = nil
		} else {
			expiresAt := time.Unix(*input.ExpiresAt, 0)
			account.ExpiresAt = &expiresAt
		}
	}
	if input.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *input.AutoPauseOnExpired
	}

	if input.TempUnschedulableEnabled != nil || input.TempUnschedulableRules != nil {
		credentials := cloneContributionMap(account.Credentials)
		if credentials == nil {
			credentials = map[string]any{}
		}
		if input.TempUnschedulableEnabled != nil {
			credentials["temp_unschedulable_enabled"] = *input.TempUnschedulableEnabled
		}
		if input.TempUnschedulableRules != nil {
			rules, err := sanitizeContributionTempUnschedulableRules(*input.TempUnschedulableRules)
			if err != nil {
				return nil, err
			}
			credentials["temp_unschedulable_rules"] = rules
		}
		account.Credentials = credentials
	}

	if input.AutoPause5hThreshold != nil || input.AutoPause7dThreshold != nil || input.AutoPause5hDisabled != nil || input.AutoPause7dDisabled != nil {
		extra := cloneContributionMap(account.Extra)
		if extra == nil {
			extra = map[string]any{}
		}
		if input.AutoPause5hThreshold != nil {
			threshold, err := normalizeContributionAutoPauseThreshold(*input.AutoPause5hThreshold, "auto_pause_5h_threshold")
			if err != nil {
				return nil, err
			}
			setOrDeleteContributionThreshold(extra, "auto_pause_5h_threshold", threshold)
		}
		if input.AutoPause7dThreshold != nil {
			threshold, err := normalizeContributionAutoPauseThreshold(*input.AutoPause7dThreshold, "auto_pause_7d_threshold")
			if err != nil {
				return nil, err
			}
			setOrDeleteContributionThreshold(extra, "auto_pause_7d_threshold", threshold)
		}
		if input.AutoPause5hDisabled != nil {
			setOrDeleteContributionBool(extra, "auto_pause_5h_disabled", *input.AutoPause5hDisabled)
		}
		if input.AutoPause7dDisabled != nil {
			setOrDeleteContributionBool(extra, "auto_pause_7d_disabled", *input.AutoPause7dDisabled)
		}
		account.Extra = extra
	}

	previousGroupIDs := append([]int64(nil), account.GroupIDs...)
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if s.schedulerRefresher != nil {
		if err := s.schedulerRefresher.RefreshAccount(ctx, updated, previousGroupIDs, "contribution_config_update"); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *AccountContributionService) Revoke(ctx context.Context, userID, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || *account.OwnerUserID != userID {
		return nil, ErrContributionOwnership
	}
	if account.ContributionStatus == ContributionStatusRevoked {
		if err := s.deleteContributionOwnedProxy(ctx, contributionOwnedProxyIDFromExtra(account)); err != nil {
			return nil, err
		}
		return account, nil
	}
	now := time.Now()
	ownedProxyID := contributionOwnedProxyID(account)
	account.ContributionStatus = ContributionStatusRevoked
	account.ContributionRevokedAt = &now
	account.Status = StatusDisabled
	account.Schedulable = false
	if ownedProxyID != nil {
		account.ProxyID = nil
	}
	previousGroupIDs := append([]int64(nil), account.GroupIDs...)
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if s.schedulerRefresher != nil {
		if err := s.schedulerRefresher.RefreshAccount(ctx, updated, previousGroupIDs, "contribution_revoke"); err != nil {
			return nil, err
		}
	}
	if err := s.deleteContributionOwnedProxy(ctx, ownedProxyID); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *AccountContributionService) Republish(ctx context.Context, userID, accountID int64) (*Account, error) {
	account, err := s.GetMine(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if account.ContributionStatus != ContributionStatusRevoked {
		return nil, ErrContributionInvalidStatus
	}

	now := time.Now()
	account.ContributionStatus = ContributionStatusPending
	account.ContributionSubmittedAt = &now
	account.ContributionApprovedAt = nil
	account.ContributionRevokedAt = nil
	account.Status = StatusDisabled
	account.Schedulable = false

	previousGroupIDs := append([]int64(nil), account.GroupIDs...)
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if s.schedulerRefresher != nil {
		if err := s.schedulerRefresher.RefreshAccount(ctx, updated, previousGroupIDs, "contribution_republish"); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *AccountContributionService) ListRewards(ctx context.Context, userID int64, params pagination.PaginationParams) ([]ContributorRewardLog, *pagination.PaginationResult, error) {
	return s.rewardRepo.ListByOwner(ctx, userID, params)
}

func (s *AccountContributionService) GetRewardSummary(ctx context.Context, userID int64) (ContributorRewardSummary, error) {
	if userID <= 0 {
		return ContributorRewardSummary{}, ErrUserNotFound
	}
	return s.rewardRepo.SummaryByOwner(ctx, userID, time.Now())
}

func (s *AccountContributionService) ListByStatus(ctx context.Context, status string, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = ContributionStatusPending
	}
	if status != "all" && status != ContributionStatusPending && status != ContributionStatusApproved && status != ContributionStatusRejected && status != ContributionStatusRevoked {
		return nil, nil, infraerrors.BadRequest("CONTRIBUTION_STATUS_INVALID", "invalid contribution status")
	}
	return s.accountRepo.ListContributionsByStatus(ctx, status, params)
}

func (s *AccountContributionService) ListPending(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return s.ListByStatus(ctx, ContributionStatusPending, params)
}

func (s *AccountContributionService) Approve(ctx context.Context, accountID int64, input ApproveContributionInput) (*Account, error) {
	if len(input.GroupIDs) == 0 {
		return nil, infraerrors.BadRequest("CONTRIBUTION_GROUP_REQUIRED", "group_ids is required")
	}
	if err := validatePositiveIDs(input.GroupIDs); err != nil {
		return nil, err
	}
	for _, gid := range input.GroupIDs {
		if _, err := s.groupRepo.GetByIDLite(ctx, gid); err != nil {
			return nil, err
		}
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || account.ContributionStatus != ContributionStatusPending {
		return nil, ErrContributionInvalidStatus
	}
	now := time.Now()
	account.ContributionStatus = ContributionStatusApproved
	account.ContributionApprovedAt = &now
	account.Status = StatusActive
	account.Schedulable = true
	if input.Concurrency != nil {
		if *input.Concurrency < 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_INVALID_CONCURRENCY", "concurrency must be >= 0")
		}
		account.Concurrency = *input.Concurrency
	}
	if input.Priority != nil {
		account.Priority = *input.Priority
	}
	previousGroupIDs := append([]int64(nil), account.GroupIDs...)
	account.GroupIDs = input.GroupIDs
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	if err := s.accountRepo.BindGroups(ctx, account.ID, input.GroupIDs); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if s.schedulerRefresher != nil {
		groupIDs := append(previousGroupIDs, input.GroupIDs...)
		if err := s.schedulerRefresher.RefreshAccount(ctx, updated, groupIDs, "contribution_approve"); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *AccountContributionService) Reject(ctx context.Context, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerUserID == nil || account.ContributionStatus != ContributionStatusPending {
		return nil, ErrContributionInvalidStatus
	}
	ownedProxyID := contributionOwnedProxyID(account)
	previousGroupIDs := append([]int64(nil), account.GroupIDs...)
	account.ContributionStatus = ContributionStatusRejected
	account.Status = StatusDisabled
	account.Schedulable = false
	if ownedProxyID != nil {
		account.ProxyID = nil
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if err := s.deleteContributionOwnedProxy(ctx, ownedProxyID); err != nil {
		return nil, err
	}
	if s.schedulerRefresher != nil {
		if err := s.schedulerRefresher.RefreshAccount(ctx, updated, previousGroupIDs, "contribution_reject"); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func cloneContributionMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func sanitizeContributionTempUnschedulableRules(input []TempUnschedulableRule) ([]map[string]any, error) {
	const maxRules = 50
	if len(input) > maxRules {
		return nil, infraerrors.BadRequest("CONTRIBUTION_TEMP_UNSCHED_RULES_TOO_MANY", "temp unschedulable rules must be <= 50")
	}
	out := make([]map[string]any, 0, len(input))
	for i := range input {
		rule := input[i]
		if rule.ErrorCode <= 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_TEMP_UNSCHED_RULE_INVALID", "error_code must be > 0")
		}
		if rule.DurationMinutes <= 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_TEMP_UNSCHED_RULE_INVALID", "duration_minutes must be > 0")
		}
		keywords := make([]string, 0, len(rule.Keywords))
		seen := map[string]struct{}{}
		for _, keyword := range rule.Keywords {
			trimmed := strings.TrimSpace(keyword)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			keywords = append(keywords, trimmed)
		}
		if len(keywords) == 0 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_TEMP_UNSCHED_RULE_INVALID", "keywords is required")
		}
		out = append(out, map[string]any{
			"error_code":       rule.ErrorCode,
			"keywords":         keywords,
			"duration_minutes": rule.DurationMinutes,
			"description":      strings.TrimSpace(rule.Description),
		})
	}
	return out, nil
}

func normalizeContributionAutoPauseThreshold(value float64, field string) (*float64, error) {
	if value <= 0 {
		return nil, nil
	}
	if value > 1 {
		if value > 100 {
			return nil, infraerrors.BadRequest("CONTRIBUTION_INVALID_AUTO_PAUSE_THRESHOLD", field+" must be between 0 and 1, or 0 and 100 when using percent")
		}
		value = value / 100
	}
	return &value, nil
}

func setOrDeleteContributionThreshold(extra map[string]any, key string, threshold *float64) {
	if threshold == nil {
		delete(extra, key)
		return
	}
	extra[key] = *threshold
}

func setOrDeleteContributionBool(extra map[string]any, key string, enabled bool) {
	if enabled {
		extra[key] = true
		return
	}
	delete(extra, key)
}

func buildOpenAIContributionProxyMap(proxies []OpenAIJSONContributionProxy) (map[string]OpenAIJSONContributionProxy, error) {
	out := make(map[string]OpenAIJSONContributionProxy, len(proxies))
	for i := range proxies {
		item := proxies[i]
		key := strings.TrimSpace(item.ProxyKey)
		if key == "" {
			key = buildContributionProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password)
		}
		if key == "" {
			return nil, infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_KEY_REQUIRED", "proxy_key is required")
		}
		if err := validateOpenAIJSONContributionProxy(item); err != nil {
			return nil, err
		}
		out[key] = item
	}
	return out, nil
}

func contributionAccountProxyKey(item OpenAIJSONContributionAccount) string {
	if item.ProxyKey == nil {
		return ""
	}
	return strings.TrimSpace(*item.ProxyKey)
}

func buildContributionProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func validateOpenAIJSONContributionProxy(item OpenAIJSONContributionProxy) error {
	protocol := strings.ToLower(strings.TrimSpace(item.Protocol))
	switch protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_PROTOCOL_UNSUPPORTED", "proxy protocol must be http, https, socks5 or socks5h")
	}
	if strings.TrimSpace(item.Host) == "" {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_HOST_REQUIRED", "proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_PORT_INVALID", "proxy port is invalid")
	}
	if len(item.Username) > 100 || len(item.Password) > 100 {
		return infraerrors.BadRequest("CONTRIBUTION_IMPORT_PROXY_AUTH_TOO_LONG", "proxy username/password is too long")
	}
	return nil
}

func (s *AccountContributionService) createContributionOwnedProxy(ctx context.Context, userID int64, accountName, rawProxyURL string) (*int64, error) {
	trimmed := strings.TrimSpace(rawProxyURL)
	if trimmed == "" {
		return nil, nil
	}
	if s.proxyRepo == nil {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_UNAVAILABLE", "contribution proxy is not available")
	}
	proxy, err := parseContributionProxyURL(trimmed)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(accountName)
	if name == "" {
		name = "OpenAI Contribution"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	proxy.Name = fmt.Sprintf("User Contribution #%d %s", userID, name)
	if len(proxy.Name) > 100 {
		proxy.Name = proxy.Name[:100]
	}
	proxy.Status = StatusActive
	proxy.FallbackMode = FallbackModeNone
	proxy.ExpiryWarnDays = 7
	if err := s.proxyRepo.Create(ctx, proxy); err != nil {
		return nil, err
	}
	return &proxy.ID, nil
}

func (s *AccountContributionService) createContributionOwnedProxyFromImport(ctx context.Context, userID int64, accountName string, item OpenAIJSONContributionProxy) (*int64, error) {
	if s == nil || s.proxyRepo == nil {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_UNAVAILABLE", "contribution proxy is not available")
	}
	if err := validateOpenAIJSONContributionProxy(item); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(accountName)
	if name == "" {
		name = strings.TrimSpace(item.Name)
	}
	if name == "" {
		name = "OpenAI Contribution"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	proxy := &Proxy{
		Name:           fmt.Sprintf("User Contribution #%d %s", userID, name),
		Protocol:       strings.ToLower(strings.TrimSpace(item.Protocol)),
		Host:           strings.TrimSpace(item.Host),
		Port:           item.Port,
		Username:       item.Username,
		Password:       item.Password,
		Status:         StatusActive,
		FallbackMode:   FallbackModeNone,
		ExpiryWarnDays: 7,
	}
	if len(proxy.Name) > 100 {
		proxy.Name = proxy.Name[:100]
	}
	if err := s.proxyRepo.Create(ctx, proxy); err != nil {
		return nil, err
	}
	return &proxy.ID, nil
}

func parseContributionProxyURL(raw string) (*Proxy, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_INVALID", "proxy url is required")
	}
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_INVALID", "invalid proxy url")
	}
	protocol := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_PROTOCOL_UNSUPPORTED", "proxy protocol must be http, https, socks5 or socks5h")
	}
	portRaw := parsed.Port()
	if portRaw == "" {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_PORT_REQUIRED", "proxy port is required")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port <= 0 || port > 65535 {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_PORT_INVALID", "proxy port is invalid")
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	if len(username) > 100 || len(password) > 100 {
		return nil, infraerrors.BadRequest("CONTRIBUTION_PROXY_AUTH_TOO_LONG", "proxy username/password is too long")
	}
	return &Proxy{
		Protocol: protocol,
		Host:     parsed.Hostname(),
		Port:     port,
		Username: username,
		Password: password,
	}, nil
}

func markContributionOwnedProxyExtra(extra map[string]any, proxyID *int64) {
	if extra == nil || proxyID == nil || *proxyID <= 0 {
		return
	}
	extra["contribution_owned_proxy_id"] = *proxyID
	extra["contribution_owned_proxy"] = true
}

func contributionOwnedProxyID(account *Account) *int64 {
	if account == nil || account.ProxyID == nil {
		return nil
	}
	id := contributionOwnedProxyIDFromExtra(account)
	if id == nil || *id != *account.ProxyID {
		return nil
	}
	return id
}

func contributionOwnedProxyIDFromExtra(account *Account) *int64 {
	if account == nil || account.Extra == nil {
		return nil
	}
	if owned, ok := account.Extra["contribution_owned_proxy"].(bool); !ok || !owned {
		return nil
	}
	raw, ok := account.Extra["contribution_owned_proxy_id"]
	if !ok {
		return nil
	}
	var id int64
	switch v := raw.(type) {
	case int64:
		id = v
	case int:
		id = int64(v)
	case float64:
		id = int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return nil
		}
		id = parsed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil
		}
		id = parsed
	default:
		return nil
	}
	if id <= 0 {
		return nil
	}
	return &id
}

func (s *AccountContributionService) deleteContributionOwnedProxy(ctx context.Context, proxyID *int64) error {
	if s == nil || s.proxyRepo == nil || proxyID == nil || *proxyID <= 0 {
		return nil
	}
	return s.proxyRepo.Delete(ctx, *proxyID)
}

func contributionIdentityKey(credentials map[string]any) string {
	chatgptUserID := strings.TrimSpace(fmt.Sprint(credentials["chatgpt_user_id"]))
	chatgptAccountID := strings.TrimSpace(fmt.Sprint(credentials["chatgpt_account_id"]))
	if chatgptUserID != "" && chatgptUserID != "<nil>" && chatgptAccountID != "" && chatgptAccountID != "<nil>" {
		return chatgptUserID + ":" + chatgptAccountID
	}
	accessToken := strings.TrimSpace(fmt.Sprint(credentials["access_token"]))
	if accessToken == "" || accessToken == "<nil>" {
		return ""
	}
	sum := sha256.Sum256([]byte(accessToken))
	return "token_sha256:" + hex.EncodeToString(sum[:])
}

func validatePositiveIDs(ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return infraerrors.BadRequest("IDS_INVALID", "ids must be positive")
		}
	}
	return nil
}
