package service

const (
	ConsumptionConcurrencyDefault = 10
)

// ConsumptionConcurrencyTier returns the highest lifetime-consumption tier
// reached by a user. Consumption is measured in billed USD.
func ConsumptionConcurrencyTier(lifetimeUSD float64) int {
	switch {
	case lifetimeUSD >= 500:
		return 4
	case lifetimeUSD >= 200:
		return 3
	case lifetimeUSD >= 100:
		return 2
	case lifetimeUSD >= 50:
		return 1
	default:
		return 0
	}
}

func ConsumptionConcurrencyBonus(tier int) int {
	switch {
	case tier >= 4:
		return 30
	case tier == 3:
		return 20
	case tier == 2:
		return 10
	case tier == 1:
		return 5
	default:
		return 0
	}
}
