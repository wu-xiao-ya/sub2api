package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// ImageUpstreamCostPerImageDefault is the default upstream cost used by
	// cost/profit reporting for one generated image.
	ImageUpstreamCostPerImageDefault = 0.001
	imageUpstreamCostPerImageMax     = 1_000_000
	imageUpstreamCostDecimalPlaces   = 10
)

// ImageUpstreamCostSettings is the public shape of the image cost setting API.
type ImageUpstreamCostSettings struct {
	CostPerImage               float64                                 `json:"cost_per_image"`
	AccountOverrides           []ImageUpstreamCostAccountOverride      `json:"account_overrides"`
	ModelOverrides             []ImageUpstreamCostModelOverride        `json:"model_overrides"`
	AccountModelOverrides      []ImageUpstreamCostAccountModelOverride `json:"account_model_overrides"`
	IgnoreUpstreamRateSnapshot bool                                    `json:"ignore_upstream_rate_snapshot"`
	BillingMode                string                                  `json:"billing_mode"`
	Unit                       string                                  `json:"unit"`
}

// ImageUpstreamCostAccountOverride overrides the default image upstream cost
// for one account. Account IDs remain stable when an operator renames an
// account in the admin UI.
type ImageUpstreamCostAccountOverride struct {
	AccountID    int64   `json:"account_id"`
	CostPerImage float64 `json:"cost_per_image"`
}

// ImageUpstreamCostModelOverride overrides the image upstream cost for one
// model. Tiers are keyed by billing size (1K/2K/4K); an empty tier map leaves
// that model to the lower-priority sources.
type ImageUpstreamCostModelOverride struct {
	Model string             `json:"model"`
	Tiers map[string]float64 `json:"tiers"`
}

// ImageUpstreamCostAccountModelOverride overrides the image upstream cost for
// one model on one account. It has the highest priority.
type ImageUpstreamCostAccountModelOverride struct {
	AccountID int64              `json:"account_id"`
	Model     string             `json:"model"`
	Tiers     map[string]float64 `json:"tiers"`
}

// ImageUpstreamCostAccountCandidate is an account that recently generated
// images, offered so operators select one by name instead of looking up its ID.
type ImageUpstreamCostAccountCandidate struct {
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
}

// ImageUpstreamCostCandidates holds the pickers' data. Models come from real
// usage because upstream providers expose image model IDs that no shipped
// catalog contains.
type ImageUpstreamCostCandidates struct {
	Models   []string                            `json:"models"`
	Accounts []ImageUpstreamCostAccountCandidate `json:"accounts"`
}

// GetImageUpstreamCostCandidates returns the image models and accounts seen in
// recent usage. It degrades to empty lists on error so a reporting hiccup
// cannot block the settings page, which keeps its manual entry paths.
func (s *SettingService) GetImageUpstreamCostCandidates(ctx context.Context) *ImageUpstreamCostCandidates {
	candidates := &ImageUpstreamCostCandidates{
		Models:   []string{},
		Accounts: []ImageUpstreamCostAccountCandidate{},
	}
	if s == nil || s.usageLogRepo == nil {
		return candidates
	}
	if models, err := s.usageLogRepo.ListImageGenerationModels(ctx); err == nil && len(models) > 0 {
		candidates.Models = models
	}
	if accounts, err := s.usageLogRepo.ListImageGenerationAccounts(ctx); err == nil && len(accounts) > 0 {
		candidates.Accounts = accounts
	}
	return candidates
}

// GetImageUpstreamCostPerImage returns the configured upstream cost per image.
// Missing, malformed, or unavailable settings fall back to the safe default so
// a settings problem cannot break dashboard cost queries.
func (s *SettingService) GetImageUpstreamCostPerImage(ctx context.Context) float64 {
	if s == nil || s.settingRepo == nil {
		return ImageUpstreamCostPerImageDefault
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyImageUpstreamCostPerImage)
	if err != nil || strings.TrimSpace(raw) == "" {
		return ImageUpstreamCostPerImageDefault
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || !isValidImageUpstreamCost(value) {
		return ImageUpstreamCostPerImageDefault
	}
	return value
}

// GetImageUpstreamCostSettings returns the configured image cost and its
// billing semantics for the admin API.
func (s *SettingService) GetImageUpstreamCostSettings(ctx context.Context) *ImageUpstreamCostSettings {
	return &ImageUpstreamCostSettings{
		CostPerImage:               s.GetImageUpstreamCostPerImage(ctx),
		AccountOverrides:           s.GetImageUpstreamCostAccountOverrides(ctx),
		ModelOverrides:             s.GetImageUpstreamCostModelOverrides(ctx),
		AccountModelOverrides:      s.GetImageUpstreamCostAccountModelOverrides(ctx),
		IgnoreUpstreamRateSnapshot: s.GetImageCostIgnoreUpstreamRateSnapshot(ctx),
		BillingMode:                string(BillingModeImage),
		Unit:                       "USD/image",
	}
}

// GetImageCostIgnoreUpstreamRateSnapshot reports whether per-image upstream cost
// should skip the upstream billing rate snapshot multiplier. Missing or
// unparsable configuration keeps the historical (multiplied) behavior.
func (s *SettingService) GetImageCostIgnoreUpstreamRateSnapshot(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyImageCostIgnoreUpstreamRateSnapshot)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

// SetImageUpstreamCostPerImage validates and persists the upstream cost used by
// cost/profit reporting. Zero is allowed when an operator intentionally wants
// to report image requests as free upstream usage.
func (s *SettingService) SetImageUpstreamCostPerImage(ctx context.Context, value float64) error {
	return s.UpdateImageUpstreamCostSettings(ctx, &value, nil, nil, nil, nil)
}

// GetImageUpstreamCostAccountOverrides returns valid account-specific image
// cost overrides. Malformed persisted data is ignored so a bad setting cannot
// break dashboard cost reporting.
func (s *SettingService) GetImageUpstreamCostAccountOverrides(ctx context.Context) []ImageUpstreamCostAccountOverride {
	if s == nil || s.settingRepo == nil {
		return []ImageUpstreamCostAccountOverride{}
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyImageUpstreamCostByAccount)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []ImageUpstreamCostAccountOverride{}
	}

	var persisted map[string]float64
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return []ImageUpstreamCostAccountOverride{}
	}

	overrides := make([]ImageUpstreamCostAccountOverride, 0, len(persisted))
	for accountIDRaw, costPerImage := range persisted {
		accountID, err := strconv.ParseInt(accountIDRaw, 10, 64)
		if err != nil || accountID <= 0 || !isValidImageUpstreamCost(costPerImage) {
			continue
		}
		overrides = append(overrides, ImageUpstreamCostAccountOverride{
			AccountID:    accountID,
			CostPerImage: costPerImage,
		})
	}
	sort.Slice(overrides, func(i, j int) bool {
		return overrides[i].AccountID < overrides[j].AccountID
	})
	return overrides
}

// UpdateImageUpstreamCostSettings updates the default and/or the override
// levels together. A nil field leaves that setting unchanged; a non-nil empty
// slice explicitly clears that override level.
func (s *SettingService) UpdateImageUpstreamCostSettings(
	ctx context.Context,
	costPerImage *float64,
	accountOverrides *[]ImageUpstreamCostAccountOverride,
	modelOverrides *[]ImageUpstreamCostModelOverride,
	accountModelOverrides *[]ImageUpstreamCostAccountModelOverride,
	ignoreUpstreamRateSnapshot *bool,
) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("setting repository is unavailable")
	}

	updates := make(map[string]string, 5)
	if costPerImage != nil {
		if !isValidImageUpstreamCost(*costPerImage) {
			return invalidImageUpstreamCostError()
		}
		updates[SettingKeyImageUpstreamCostPerImage] = strconv.FormatFloat(*costPerImage, 'f', -1, 64)
	}
	if accountOverrides != nil {
		encoded, err := encodeImageUpstreamCostAccountOverrides(*accountOverrides)
		if err != nil {
			return err
		}
		updates[SettingKeyImageUpstreamCostByAccount] = encoded
	}
	if modelOverrides != nil {
		encoded, err := encodeImageUpstreamCostModelOverrides(*modelOverrides)
		if err != nil {
			return err
		}
		updates[SettingKeyImageUpstreamCostByModel] = encoded
	}
	if accountModelOverrides != nil {
		encoded, err := encodeImageUpstreamCostAccountModelOverrides(*accountModelOverrides)
		if err != nil {
			return err
		}
		updates[SettingKeyImageUpstreamCostByAccountModel] = encoded
	}
	if ignoreUpstreamRateSnapshot != nil {
		updates[SettingKeyImageCostIgnoreUpstreamRateSnapshot] = strconv.FormatBool(*ignoreUpstreamRateSnapshot)
	}
	if len(updates) == 0 {
		return infraerrors.BadRequest(
			"INVALID_IMAGE_UPSTREAM_COST",
			"at least one of cost_per_image, account_overrides, model_overrides, account_model_overrides or ignore_upstream_rate_snapshot is required",
		)
	}
	return s.settingRepo.SetMultiple(ctx, updates)
}

func encodeImageUpstreamCostModelOverrides(overrides []ImageUpstreamCostModelOverride) (string, error) {
	persisted := make(map[string]map[string]float64, len(overrides))
	for _, override := range overrides {
		model := strings.TrimSpace(override.Model)
		if model == "" {
			return "", infraerrors.BadRequest("INVALID_IMAGE_UPSTREAM_COST", "model_overrides entries require a model")
		}
		tiers, err := normalizeImageCostTiers(override.Tiers)
		if err != nil {
			return "", err
		}
		if len(tiers) == 0 {
			return "", infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("model_overrides entry %q requires at least one of 1K/2K/4K", model),
			)
		}
		if _, exists := persisted[model]; exists {
			return "", infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("model_overrides must not contain duplicate model %q", model),
			)
		}
		persisted[model] = tiers
	}
	encoded, err := json.Marshal(persisted)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func encodeImageUpstreamCostAccountModelOverrides(overrides []ImageUpstreamCostAccountModelOverride) (string, error) {
	persisted := make(map[string]map[string]map[string]float64, len(overrides))
	for _, override := range overrides {
		if override.AccountID <= 0 {
			return "", infraerrors.BadRequest("INVALID_IMAGE_UPSTREAM_COST", "account_model_overrides entries require a positive account_id")
		}
		model := strings.TrimSpace(override.Model)
		if model == "" {
			return "", infraerrors.BadRequest("INVALID_IMAGE_UPSTREAM_COST", "account_model_overrides entries require a model")
		}
		tiers, err := normalizeImageCostTiers(override.Tiers)
		if err != nil {
			return "", err
		}
		if len(tiers) == 0 {
			return "", infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("account_model_overrides entry %d/%q requires at least one of 1K/2K/4K", override.AccountID, model),
			)
		}
		accountKey := strconv.FormatInt(override.AccountID, 10)
		if persisted[accountKey] == nil {
			persisted[accountKey] = make(map[string]map[string]float64)
		}
		if _, exists := persisted[accountKey][model]; exists {
			return "", infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("account_model_overrides must not contain duplicate %d/%q", override.AccountID, model),
			)
		}
		persisted[accountKey][model] = tiers
	}
	encoded, err := json.Marshal(persisted)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func encodeImageUpstreamCostAccountOverrides(overrides []ImageUpstreamCostAccountOverride) (string, error) {
	persisted := make(map[string]float64, len(overrides))
	for _, override := range overrides {
		if override.AccountID <= 0 || !isValidImageUpstreamCost(override.CostPerImage) {
			return "", invalidImageUpstreamCostError()
		}
		key := strconv.FormatInt(override.AccountID, 10)
		if _, exists := persisted[key]; exists {
			return "", infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				"account_overrides must not contain duplicate account_id values",
			)
		}
		persisted[key] = override.CostPerImage
	}
	encoded, err := json.Marshal(persisted)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func invalidImageUpstreamCostError() error {
	return infraerrors.BadRequest(
		"INVALID_IMAGE_UPSTREAM_COST",
		"cost_per_image must be a finite non-negative number with at most 10 decimal places",
	)
}

// normalizeImageCostModelKey canonicalizes a model identifier used as a lookup
// key so stored overrides and query-time lookups agree on case and padding.
func normalizeImageCostModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// normalizeImageCostTiers validates a tier map and canonicalizes tier keys to
// 1K/2K/4K. Empty maps and nil values are dropped so a partially configured
// model simply falls through to lower-priority sources.
func normalizeImageCostTiers(tiers map[string]float64) (map[string]float64, error) {
	if len(tiers) == 0 {
		return map[string]float64{}, nil
	}
	out := make(map[string]float64, len(tiers))
	for rawTier, cost := range tiers {
		tier, ok := ClassifyImageBillingTier(rawTier)
		if !ok {
			// "auto"/"" describe an unresolved size, not a billable tier.
			return nil, infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("unsupported image cost tier %q; use 1K, 2K or 4K", rawTier),
			)
		}
		if _, exists := out[tier]; exists {
			return nil, infraerrors.BadRequest(
				"INVALID_IMAGE_UPSTREAM_COST",
				fmt.Sprintf("duplicate image cost tier %q", tier),
			)
		}
		if !isValidImageUpstreamCost(cost) {
			return nil, invalidImageUpstreamCostError()
		}
		out[tier] = cost
	}
	return out, nil
}

// GetImageUpstreamCostModelOverrides returns valid per-model image cost
// overrides. Malformed persisted data is ignored rather than breaking reports.
func (s *SettingService) GetImageUpstreamCostModelOverrides(ctx context.Context) []ImageUpstreamCostModelOverride {
	if s == nil || s.settingRepo == nil {
		return []ImageUpstreamCostModelOverride{}
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyImageUpstreamCostByModel)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []ImageUpstreamCostModelOverride{}
	}
	var persisted map[string]map[string]float64
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return []ImageUpstreamCostModelOverride{}
	}

	overrides := make([]ImageUpstreamCostModelOverride, 0, len(persisted))
	for rawModel, tiers := range persisted {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			continue
		}
		normalized, err := normalizeImageCostTiers(tiers)
		if err != nil || len(normalized) == 0 {
			continue
		}
		overrides = append(overrides, ImageUpstreamCostModelOverride{Model: model, Tiers: normalized})
	}
	sort.Slice(overrides, func(i, j int) bool {
		return normalizeImageCostModelKey(overrides[i].Model) < normalizeImageCostModelKey(overrides[j].Model)
	})
	return overrides
}

// GetImageUpstreamCostAccountModelOverrides returns valid account-and-model
// image cost overrides, sorted for stable admin display.
func (s *SettingService) GetImageUpstreamCostAccountModelOverrides(ctx context.Context) []ImageUpstreamCostAccountModelOverride {
	if s == nil || s.settingRepo == nil {
		return []ImageUpstreamCostAccountModelOverride{}
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyImageUpstreamCostByAccountModel)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []ImageUpstreamCostAccountModelOverride{}
	}
	var persisted map[string]map[string]map[string]float64
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return []ImageUpstreamCostAccountModelOverride{}
	}

	overrides := make([]ImageUpstreamCostAccountModelOverride, 0, len(persisted))
	for rawAccountID, models := range persisted {
		accountID, err := strconv.ParseInt(rawAccountID, 10, 64)
		if err != nil || accountID <= 0 {
			continue
		}
		for rawModel, tiers := range models {
			model := strings.TrimSpace(rawModel)
			if model == "" {
				continue
			}
			normalized, err := normalizeImageCostTiers(tiers)
			if err != nil || len(normalized) == 0 {
				continue
			}
			overrides = append(overrides, ImageUpstreamCostAccountModelOverride{
				AccountID: accountID,
				Model:     model,
				Tiers:     normalized,
			})
		}
	}
	sort.Slice(overrides, func(i, j int) bool {
		if overrides[i].AccountID != overrides[j].AccountID {
			return overrides[i].AccountID < overrides[j].AccountID
		}
		return normalizeImageCostModelKey(overrides[i].Model) < normalizeImageCostModelKey(overrides[j].Model)
	})
	return overrides
}

// ResolveImageUpstreamCost picks the upstream cost for one generated image,
// applying the documented priority order:
//
//	account+model+tier > model+tier > account > global default
//
// A tier that is configured but has no entry for the requested size does not
// fall through to a lower tier of the same level; it continues down the
// priority chain so an operator can specify only the sizes they care about.
func (s *SettingService) ResolveImageUpstreamCost(ctx context.Context, accountID int64, model string, imageSize string) float64 {
	tier := NormalizeImageBillingTierOrDefault(imageSize)
	modelKey := normalizeImageCostModelKey(model)

	if accountID > 0 && modelKey != "" {
		if cost, ok := lookupAccountModelImageCost(s.GetImageUpstreamCostAccountModelOverrides(ctx), accountID, modelKey, tier); ok {
			return cost
		}
	}
	if modelKey != "" {
		if cost, ok := lookupModelImageCost(s.GetImageUpstreamCostModelOverrides(ctx), modelKey, tier); ok {
			return cost
		}
	}
	if accountID > 0 {
		for _, override := range s.GetImageUpstreamCostAccountOverrides(ctx) {
			if override.AccountID == accountID {
				return override.CostPerImage
			}
		}
	}
	return s.GetImageUpstreamCostPerImage(ctx)
}

func lookupAccountModelImageCost(overrides []ImageUpstreamCostAccountModelOverride, accountID int64, modelKey, tier string) (float64, bool) {
	for _, override := range overrides {
		if override.AccountID != accountID || normalizeImageCostModelKey(override.Model) != modelKey {
			continue
		}
		if cost, ok := override.Tiers[tier]; ok {
			return cost, true
		}
	}
	return 0, false
}

func lookupModelImageCost(overrides []ImageUpstreamCostModelOverride, modelKey, tier string) (float64, bool) {
	for _, override := range overrides {
		if normalizeImageCostModelKey(override.Model) != modelKey {
			continue
		}
		if cost, ok := override.Tiers[tier]; ok {
			return cost, true
		}
	}
	return 0, false
}

func isValidImageUpstreamCost(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > imageUpstreamCostPerImageMax {
		return false
	}
	scale := math.Pow10(imageUpstreamCostDecimalPlaces)
	return math.Abs(value*scale-math.Round(value*scale)) < 1e-6
}
