package service

import "context"

const (
	SettingKeyImageUpstreamCostPerImage           = "image_upstream_cost_per_image"
	SettingKeyImageUpstreamCostByAccount          = "image_upstream_cost_by_account"
	SettingKeyImageUpstreamCostByModel            = "image_upstream_cost_by_model"
	SettingKeyImageUpstreamCostByAccountModel     = "image_upstream_cost_by_account_model"
	SettingKeyImageCostIgnoreUpstreamRateSnapshot = "image_cost_ignore_upstream_rate_snapshot"
)

type imageCostCandidateReader interface {
	ListImageGenerationModels(context.Context) ([]string, error)
	ListImageGenerationAccounts(context.Context) ([]ImageUpstreamCostAccountCandidate, error)
}
