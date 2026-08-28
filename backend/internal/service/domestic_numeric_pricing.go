package service

import (
	"strings"
	"time"
)

const deepSeekOffPeakMultiplier = 0.5

var chinaStandardTime = time.FixedZone("Asia/Shanghai", 8*60*60)

// domesticNumericPricing returns the site's USD billing card for domestic
// models whose official mainland price is published in CNY. The numeric value
// is carried over unchanged: an official CNY 8 / MTok input price is billed as
// USD 8 / MTok on this site.
func domesticNumericPricing(model string) *ModelPricing {
	model = normalizeDomesticPricingModel(model)

	switch model {
	case "deepseek-v4-flash", "deepseek-v4-flash-0731", "deepseek-v4-flash-vision-exp":
		return tokenPricing(3, 9, 0.1)
	case "deepseek-v4-pro", "deepseek-v4-pro-0813":
		return tokenPricing(9, 27, 0.3)
	case "glm-5.1", "glm-5.2", "glm-5.3":
		return tokenPricing(8, 28, 2)
	case "glm-5.3-flash":
		return tokenPricing(0.8, 2.8, 0.23)
	case "kimi-k3":
		return tokenPricing(20, 100, 2)
	case "kimi-k2.7-code":
		return tokenPricing(6.5, 27, 1.3)
	case "kimi-k2.7code":
		return tokenPricing(6.5, 27, 1.3)
	case "kimi-k2.7-code-highspeed":
		return tokenPricing(13, 54, 2.6)
	case "kimi-k2.6":
		return tokenPricing(6.5, 27, 1.1)
	case "kimi-k2.5":
		return tokenPricing(4, 21, 0.7)
	case "minimax-m3", "minimax-m2.7":
		return tokenPricing(2, 8, 0.2)
	case "minimax-m2.7-highspeed":
		return tokenPricing(2.4, 9.6, 0.24)
	case "mimo-v2.5":
		return tokenPricing(1, 2, 0.02)
	case "mimo-v2.5-pro":
		return tokenPricing(3, 6, 0.025)
	case "hy3", "hunyuan-hy3":
		return tokenPricing(1, 4, 0.25)
	case "qwen3.7-max", "qwen3.8-max":
		return tokenPricing(12, 36, 1.2)
	case "qwen3.7-plus":
		pricing := tokenPricing(2, 8, 0.4)
		pricing.LongContextInputThreshold = 256000
		pricing.LongContextInputMultiplier = 3
		pricing.LongContextOutputMultiplier = 3
		return pricing
	default:
		return nil
	}
}

// DeepSeek V4 publishes peak prices and charges half price outside the two
// Beijing-time peak windows: [09:00,12:00) and [14:00,18:00).
func applyDomesticTimePricingAt(model string, pricing *ModelPricing, now time.Time) *ModelPricing {
	if pricing == nil || !isDeepSeekV4PricingModel(model) || isDeepSeekPeakTime(now) {
		return pricing
	}

	cloned := *pricing
	cloned.InputPricePerToken *= deepSeekOffPeakMultiplier
	cloned.InputPricePerTokenPriority *= deepSeekOffPeakMultiplier
	cloned.OutputPricePerToken *= deepSeekOffPeakMultiplier
	cloned.OutputPricePerTokenPriority *= deepSeekOffPeakMultiplier
	cloned.CacheCreationPricePerToken *= deepSeekOffPeakMultiplier
	cloned.CacheCreationPricePerTokenPriority *= deepSeekOffPeakMultiplier
	cloned.CacheReadPricePerToken *= deepSeekOffPeakMultiplier
	cloned.CacheReadPricePerTokenPriority *= deepSeekOffPeakMultiplier
	cloned.CacheCreation5mPrice *= deepSeekOffPeakMultiplier
	cloned.CacheCreation1hPrice *= deepSeekOffPeakMultiplier
	return &cloned
}

func isDeepSeekV4PricingModel(model string) bool {
	switch normalizeDomesticPricingModel(model) {
	case "deepseek-v4-flash",
		"deepseek-v4-flash-0731",
		"deepseek-v4-flash-vision-exp",
		"deepseek-v4-pro",
		"deepseek-v4-pro-0813",
		"deepseek-chat",
		"deepseek-reasoner":
		return true
	default:
		return false
	}
}

func isDeepSeekPeakTime(now time.Time) bool {
	local := now.In(chinaStandardTime)
	minute := local.Hour()*60 + local.Minute()
	return (minute >= 9*60 && minute < 12*60) ||
		(minute >= 14*60 && minute < 18*60)
}

func normalizeDomesticPricingModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndex(model, "/"); slash >= 0 && slash+1 < len(model) {
		model = model[slash+1:]
	}
	return model
}

func tokenPricing(inputPerMTok, outputPerMTok, cacheReadPerMTok float64) *ModelPricing {
	return &ModelPricing{
		InputPricePerToken:     inputPerMTok * 1e-6,
		OutputPricePerToken:    outputPerMTok * 1e-6,
		CacheReadPricePerToken: cacheReadPerMTok * 1e-6,
		SupportsCacheBreakdown: false,
	}
}

func applyDomesticNumericPricing(model string, pricing *ModelPricing) *ModelPricing {
	override := domesticNumericPricing(model)
	if override == nil {
		return pricing
	}
	if pricing == nil {
		return override
	}

	cloned := *pricing
	cloned.InputPricePerToken = override.InputPricePerToken
	cloned.OutputPricePerToken = override.OutputPricePerToken
	cloned.CacheReadPricePerToken = override.CacheReadPricePerToken
	cloned.InputPricePerTokenPriority = 0
	cloned.OutputPricePerTokenPriority = 0
	cloned.CacheReadPricePerTokenPriority = 0
	cloned.LongContextInputThreshold = override.LongContextInputThreshold
	cloned.LongContextInputMultiplier = override.LongContextInputMultiplier
	cloned.LongContextOutputMultiplier = override.LongContextOutputMultiplier
	return &cloned
}
