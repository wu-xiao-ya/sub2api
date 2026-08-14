package usagestats

// ApplyCostProfitMetrics derives upstream cost, profit, and margin from the
// existing dashboard accounting fields. The repository calculates account_cost
// from time-bucketed upstream probe snapshots when available, and falls back to
// the usage-log account cost for historical rows without a snapshot.
func ApplyCostProfitMetrics(groups []GroupStat) CostProfitSummary {
	var summary CostProfitSummary

	for i := range groups {
		group := &groups[i]
		group.UpstreamCost = group.AccountCost
		if group.Cost != 0 {
			group.UpstreamMultiplier = group.UpstreamCost / group.Cost
		} else {
			group.UpstreamMultiplier = 0
		}
		group.Profit = group.ActualCost - group.UpstreamCost
		if group.ActualCost != 0 {
			group.ProfitMargin = group.Profit / group.ActualCost
		} else {
			group.ProfitMargin = 0
		}

		summary.Requests += group.Requests
		summary.TotalTokens += group.TotalTokens
		summary.StandardCost += group.Cost
		summary.ActualCost += group.ActualCost
		summary.UpstreamCost += group.UpstreamCost
		summary.Profit += group.Profit
	}

	if summary.ActualCost != 0 {
		summary.ProfitMargin = summary.Profit / summary.ActualCost
	}
	if summary.StandardCost != 0 {
		summary.UpstreamMultiplier = summary.UpstreamCost / summary.StandardCost
	}
	return summary
}
