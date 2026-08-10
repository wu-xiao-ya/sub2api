package service

import (
	"context"
	"encoding/json"
	"errors"
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
	CostPerImage     float64                            `json:"cost_per_image"`
	AccountOverrides []ImageUpstreamCostAccountOverride `json:"account_overrides"`
	BillingMode      string                             `json:"billing_mode"`
	Unit             string                             `json:"unit"`
}

// ImageUpstreamCostAccountOverride overrides the default image upstream cost
// for one account. Account IDs remain stable when an operator renames an
// account in the admin UI.
type ImageUpstreamCostAccountOverride struct {
	AccountID    int64   `json:"account_id"`
	CostPerImage float64 `json:"cost_per_image"`
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
		CostPerImage:     s.GetImageUpstreamCostPerImage(ctx),
		AccountOverrides: s.GetImageUpstreamCostAccountOverrides(ctx),
		BillingMode:      string(BillingModeImage),
		Unit:             "USD/image",
	}
}

// SetImageUpstreamCostPerImage validates and persists the upstream cost used by
// cost/profit reporting. Zero is allowed when an operator intentionally wants
// to report image requests as free upstream usage.
func (s *SettingService) SetImageUpstreamCostPerImage(ctx context.Context, value float64) error {
	return s.UpdateImageUpstreamCostSettings(ctx, &value, nil)
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

// UpdateImageUpstreamCostSettings updates the default and/or account-specific
// image upstream cost settings together. A nil field leaves that setting
// unchanged; an empty override slice explicitly clears all account overrides.
func (s *SettingService) UpdateImageUpstreamCostSettings(ctx context.Context, costPerImage *float64, accountOverrides *[]ImageUpstreamCostAccountOverride) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("setting repository is unavailable")
	}

	updates := make(map[string]string, 2)
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
	if len(updates) == 0 {
		return infraerrors.BadRequest(
			"INVALID_IMAGE_UPSTREAM_COST",
			"cost_per_image or account_overrides is required",
		)
	}
	return s.settingRepo.SetMultiple(ctx, updates)
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

func isValidImageUpstreamCost(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > imageUpstreamCostPerImageMax {
		return false
	}
	scale := math.Pow10(imageUpstreamCostDecimalPlaces)
	return math.Abs(value*scale-math.Round(value*scale)) < 1e-6
}
