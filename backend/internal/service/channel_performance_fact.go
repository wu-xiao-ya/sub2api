package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A minimal terminal witness, never a billing record or an upstream attempt log.
type ChannelPerformanceFact struct {
	APIKeyID        int64
	RequestID       string
	GroupID         int64
	Model           string
	ServiceTier     string
	ReasoningEffort string
	Stream          *bool
	Outcome         ChannelPerformanceOutcome
	StartedAt       time.Time
	CompletedAt     time.Time
	Latency         *UsageLatencyBreakdown
}

func (f *ChannelPerformanceFact) Validate() error {
	if f.APIKeyID <= 0 || f.GroupID <= 0 || strings.TrimSpace(f.RequestID) == "" || len(f.RequestID) > 512 || strings.TrimSpace(f.Model) == "" || len(f.Model) > 256 {
		return fmt.Errorf("invalid performance identity")
	}
	if f.StartedAt.IsZero() || f.CompletedAt.Before(f.StartedAt) {
		return fmt.Errorf("invalid performance timestamps")
	}
	switch f.Outcome {
	case PerformanceSuccess, PerformanceFailure, PerformanceExcluded, PerformanceUnknown:
	default:
		return fmt.Errorf("invalid performance outcome")
	}
	for _, v := range []*string{&f.ServiceTier, &f.ReasoningEffort} {
		*v = strings.TrimSpace(*v)
		if *v == "" {
			*v = "unknown"
		}
		if len(*v) > 64 {
			return fmt.Errorf("invalid performance dimension")
		}
	}
	return nil
}

type ChannelPerformanceFactRepository interface {
	RecordFacts(context.Context, []ChannelPerformanceFact) error
	ProcessPending(context.Context, time.Time) error
}
