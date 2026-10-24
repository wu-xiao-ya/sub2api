package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// UpstreamModelRatio is one per-model entry of a synced new-api /api/pricing
// snapshot. Ratio semantics follow new-api: USD cost = group_ratio *
// model_ratio * (uncached_prompt + cached_prompt * cache_ratio + completion *
// completion_ratio) / quota_per_unit for ratio-billed models, and
// model_price * group_ratio per call for per-call models.
type UpstreamModelRatio struct {
	ModelName       string         `json:"model_name"`
	QuotaType       int            `json:"quota_type"`
	ModelRatio      float64        `json:"model_ratio"`
	CompletionRatio float64        `json:"completion_ratio"`
	ModelPrice      float64        `json:"model_price"`
	CacheRatio      *float64       `json:"cache_ratio,omitempty"`
	GroupRatio      float64        `json:"group_ratio"`
	Raw             map[string]any `json:"raw,omitempty"`
}

const upstreamRatioMaximum = 1_000_000

// EffectiveCompletionRatio mirrors the new-api fallback: a ratio-billed model
// without an explicit completion ratio bills output at the input rate.
func (m UpstreamModelRatio) EffectiveCompletionRatio() float64 {
	if m.QuotaType != 0 {
		return 0
	}
	if m.CompletionRatio > 0 && !math.IsNaN(m.CompletionRatio) && !math.IsInf(m.CompletionRatio, 0) {
		return m.CompletionRatio
	}
	return 1
}

// effectiveCacheRatio mirrors the conservative new-api default: without an
// explicit cache ratio, cached prompt is billed at the full input rate.
func (m UpstreamModelRatio) effectiveCacheRatio() float64 {
	if m.CacheRatio != nil && *m.CacheRatio >= 0 && !math.IsNaN(*m.CacheRatio) && !math.IsInf(*m.CacheRatio, 0) {
		return *m.CacheRatio
	}
	return 1
}

// CostUSD returns the upstream cost in USD for one request.
func (m UpstreamModelRatio) CostUSD(tokens UsageTokens, requestCount int, quotaPerUnit float64) float64 {
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return 0
	}
	if m.QuotaType == 1 {
		calls := requestCount
		if calls < 1 {
			calls = 1
		}
		return sanitizeUpstreamRatioCost(m.ModelPrice * m.GroupRatio * float64(calls))
	}
	if m.ModelRatio <= 0 {
		return 0
	}
	billablePrompt := float64(tokens.InputTokens + tokens.CacheCreationTokens)
	if cached := float64(tokens.CacheReadTokens); cached > 0 {
		billablePrompt += cached * m.effectiveCacheRatio()
	}
	billablePrompt += float64(tokens.OutputTokens) * m.EffectiveCompletionRatio()
	return sanitizeUpstreamRatioCost(m.GroupRatio * m.ModelRatio * billablePrompt / quotaPerUnit)
}

func sanitizeUpstreamRatioCost(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return value
}

// upstreamRatioPricingResponse is the public new-api pricing payload
// (doc.newapi.pro 公共信息模块). Forks add extra fields; the parser keeps each
// entry's raw JSON so extension fields survive into the snapshot table.
type upstreamRatioPricingResponse struct {
	Success    *bool                  `json:"success"`
	Data       []json.RawMessage      `json:"data"`
	GroupRatio map[string]json.Number `json:"group_ratio"`
}

// ParseUpstreamRatioPricing decodes a /api/pricing payload. group is the
// account-configured new-api group name used to resolve the group ratio
// (falls back to "default", then to 1). Entries without a usable model name
// or without any ratio source are skipped; the error return signals a payload
// that is not a new-api pricing response at all (unsupported upstream).
func ParseUpstreamRatioPricing(body []byte, group string) ([]UpstreamModelRatio, float64, error) {
	var payload upstreamRatioPricingResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, fmt.Errorf("decode_pricing_response: %w", err)
	}
	if payload.Success != nil && !*payload.Success {
		return nil, 0, fmt.Errorf("pricing_response_unsuccessful")
	}
	if len(payload.Data) == 0 {
		return nil, 0, fmt.Errorf("pricing_data_empty")
	}
	groupRatio, err := resolveUpstreamGroupRatio(payload.GroupRatio, group)
	if err != nil {
		return nil, 0, err
	}

	entries := make([]UpstreamModelRatio, 0, len(payload.Data))
	for _, rawEntry := range payload.Data {
		entry, err := decodeUpstreamModelRatioEntry(rawEntry, groupRatio)
		if err != nil {
			if err.Error() == "missing_model_name" || err.Error() == "model_ratio_missing" {
				continue
			}
			return nil, 0, fmt.Errorf("invalid_pricing_entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("pricing_entries_unusable")
	}
	return entries, groupRatio, nil
}

func resolveUpstreamGroupRatio(groupRatio map[string]json.Number, group string) (float64, error) {
	for _, candidate := range []string{strings.TrimSpace(group), "default"} {
		if candidate == "" {
			continue
		}
		if value, ok := groupRatio[candidate]; ok {
			parsed, err := value.Float64()
			if err != nil || !validUpstreamRatio(parsed, false) {
				return 0, fmt.Errorf("group_ratio_out_of_range")
			}
			return parsed, nil
		}
	}
	return 1, nil
}

func decodeUpstreamModelRatioEntry(raw json.RawMessage, groupRatio float64) (UpstreamModelRatio, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return UpstreamModelRatio{}, err
	}
	modelName := strings.TrimSpace(flexibleUpstreamRatioString(payload["model_name"]))
	if modelName == "" {
		return UpstreamModelRatio{}, fmt.Errorf("missing_model_name")
	}
	quotaType := 0
	if rawQuotaType, exists := payload["quota_type"]; exists {
		value, ok := flexibleUpstreamRatioOptionalFloat(rawQuotaType)
		if !ok || math.Trunc(value) != value || value < 0 || value > 1 {
			return UpstreamModelRatio{}, fmt.Errorf("unsupported_quota_type")
		}
		quotaType = int(value)
	}
	modelRatio, modelRatioPresent, err := optionalUpstreamRatio(payload, "model_ratio")
	if err != nil {
		return UpstreamModelRatio{}, err
	}
	completionRatio, _, err := optionalUpstreamRatio(payload, "completion_ratio")
	if err != nil {
		return UpstreamModelRatio{}, err
	}
	modelPrice, _, err := optionalUpstreamRatio(payload, "model_price")
	if err != nil {
		return UpstreamModelRatio{}, err
	}
	entry := UpstreamModelRatio{
		ModelName:       modelName,
		QuotaType:       quotaType,
		ModelRatio:      modelRatio,
		CompletionRatio: completionRatio,
		ModelPrice:      modelPrice,
		GroupRatio:      groupRatio,
	}
	if entry.QuotaType != 0 && entry.QuotaType != 1 {
		return UpstreamModelRatio{}, fmt.Errorf("unsupported_quota_type")
	}
	if entry.QuotaType == 0 && (!validUpstreamRatio(entry.ModelRatio, false) ||
		!validUpstreamRatio(entry.CompletionRatio, true)) {
		if !modelRatioPresent {
			return UpstreamModelRatio{}, fmt.Errorf("model_ratio_missing")
		}
		return UpstreamModelRatio{}, fmt.Errorf("model_ratio_out_of_range")
	}
	if entry.QuotaType == 1 && !validUpstreamRatio(entry.ModelPrice, false) {
		return UpstreamModelRatio{}, fmt.Errorf("model_price_out_of_range")
	}
	if !validUpstreamRatio(entry.GroupRatio, false) {
		return UpstreamModelRatio{}, fmt.Errorf("group_ratio_out_of_range")
	}
	if rawCacheRatio, exists := payload["cache_ratio"]; exists && rawCacheRatio != nil {
		cacheRatio, ok := flexibleUpstreamRatioOptionalFloat(rawCacheRatio)
		if !ok {
			return UpstreamModelRatio{}, fmt.Errorf("cache_ratio_invalid")
		}
		if !validUpstreamRatio(cacheRatio, true) {
			return UpstreamModelRatio{}, fmt.Errorf("cache_ratio_out_of_range")
		}
		entry.CacheRatio = &cacheRatio
	}
	// A ratio-billed entry without any positive ratio cannot produce a cost;
	// skip it instead of silently billing upstream usage at zero.
	if entry.QuotaType == 0 && entry.ModelRatio <= 0 {
		return UpstreamModelRatio{}, fmt.Errorf("model_ratio_missing")
	}
	delete(payload, "icon")
	delete(payload, "description")
	entry.Raw = payload
	return entry, nil
}

func optionalUpstreamRatio(payload map[string]any, key string) (float64, bool, error) {
	raw, exists := payload[key]
	if !exists || raw == nil {
		return 0, false, nil
	}
	value, ok := flexibleUpstreamRatioOptionalFloat(raw)
	if !ok {
		return 0, true, fmt.Errorf("%s_invalid", key)
	}
	if !validUpstreamRatio(value, true) {
		return 0, true, fmt.Errorf("%s_out_of_range", key)
	}
	return value, true, nil
}

func validUpstreamRatio(value float64, allowZero bool) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > upstreamRatioMaximum {
		return false
	}
	return allowZero || value > 0
}

func flexibleUpstreamRatioString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func flexibleUpstreamRatioFloat(value any) float64 {
	parsed, _ := flexibleUpstreamRatioOptionalFloat(value)
	return parsed
}

func flexibleUpstreamRatioOptionalFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	case bool:
		return 0, false
	default:
		return 0, false
	}
}
