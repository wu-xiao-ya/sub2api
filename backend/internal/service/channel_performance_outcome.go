package service

// These are terminal logical-request facts, not individual upstream attempts.
// Collectors must set ownership structurally, never by matching "balance" text.
type ChannelPerformanceOutcomeInput struct {
	FinalSuccess          bool
	VoluntaryCancellation bool
	ErrorOwner            string
	NoAvailableAccount    bool
}

type ChannelPerformanceOutcome string

const (
	PerformanceSuccess  ChannelPerformanceOutcome = "success"
	PerformanceFailure  ChannelPerformanceOutcome = "failure"
	PerformanceExcluded ChannelPerformanceOutcome = "excluded"
	PerformanceUnknown  ChannelPerformanceOutcome = "unknown"
)

func ClassifyChannelPerformanceOutcome(input ChannelPerformanceOutcomeInput) ChannelPerformanceOutcome {
	if input.FinalSuccess {
		return PerformanceSuccess
	}
	if input.VoluntaryCancellation {
		return PerformanceExcluded
	}
	if input.NoAvailableAccount {
		return PerformanceFailure
	}
	switch input.ErrorOwner {
	case "client":
		return PerformanceExcluded
	case "provider", "system":
		return PerformanceFailure
	default:
		return PerformanceUnknown
	}
}
