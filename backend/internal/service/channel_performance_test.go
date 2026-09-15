package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type performanceRepoStub struct {
	mu        sync.Mutex
	calls     int
	rows      []ChannelPerformanceRow
	requested []int64
}

func TestPerformanceTerminalOutcomeOwnership(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input ChannelPerformanceOutcomeInput
		want  ChannelPerformanceOutcome
	}{
		{"user balance", ChannelPerformanceOutcomeInput{ErrorOwner: "client"}, PerformanceExcluded},
		{"upstream balance", ChannelPerformanceOutcomeInput{ErrorOwner: "provider"}, PerformanceFailure},
		{"user quota", ChannelPerformanceOutcomeInput{ErrorOwner: "client"}, PerformanceExcluded},
		{"upstream quota", ChannelPerformanceOutcomeInput{ErrorOwner: "provider"}, PerformanceFailure},
		{"account pool", ChannelPerformanceOutcomeInput{NoAvailableAccount: true}, PerformanceFailure},
		{"cancel", ChannelPerformanceOutcomeInput{VoluntaryCancellation: true}, PerformanceExcluded},
		{"stream failure", ChannelPerformanceOutcomeInput{ErrorOwner: "system"}, PerformanceFailure},
		{"recovered retry", ChannelPerformanceOutcomeInput{FinalSuccess: true, ErrorOwner: "provider"}, PerformanceSuccess},
		{"no ownership", ChannelPerformanceOutcomeInput{}, PerformanceUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, ClassifyChannelPerformanceOutcome(tc.input)) })
	}
}

func (r *performanceRepoStub) Query(_ context.Context, _ ChannelPerformanceFilter, ids []int64) ([]ChannelPerformanceRow, ChannelPerformanceCoverage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.requested = append([]int64{}, ids...)
	return r.rows, ChannelPerformanceCoverage{}, nil
}
func (*performanceRepoStub) Recompute(context.Context, time.Time, time.Time) error { return nil }
func (*performanceRepoStub) Coverage(context.Context) (ChannelPerformanceCoverage, error) {
	return ChannelPerformanceCoverage{}, nil
}

func TestPerformanceWeightedMetrics(t *testing.T) {
	var c ChannelPerformanceCounts
	c.Add(ChannelPerformanceCounts{Success: 1, Failure: 1, CharacterCount: 1, CharacterSum: 100, DurationCount: 1, DurationSum: 1000, OutputTokens: 100, OutputMs: 1000})
	c.Add(ChannelPerformanceCounts{Success: 9, Failure: 0, CharacterCount: 9, CharacterSum: 1800, DurationCount: 9, DurationSum: 18000, OutputTokens: 100, OutputMs: 9000})
	m := c.Metric()
	require.InDelta(t, 190, *m.FirstCharacterMs, 0.001)
	require.InDelta(t, 20, *m.OutputTPS, 0.001)
	require.InDelta(t, 1000.0/11, *m.SuccessRate, 0.001)
	require.Equal(t, "adequate", m.SampleQuality)
	empty := ChannelPerformanceCounts{}.Metric()
	require.Nil(t, empty.FirstCharacterMs)
	require.Nil(t, empty.DurationMs)
	require.Nil(t, empty.OutputTPS)
	require.Nil(t, empty.SuccessRate)
	require.Equal(t, "empty", empty.SampleQuality)
	require.Equal(t, "low", (ChannelPerformanceCounts{Success: 9}).Metric().SampleQuality)
}

func TestPerformancePermissionScopeAndCacheIsolation(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	f := ChannelPerformanceFilter{}
	require.NoError(t, f.Normalize(now))
	r := &performanceRepoStub{rows: []ChannelPerformanceRow{
		{GroupID: 1, Model: "model-a", At: now.Add(-time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Success: 2}},
		{GroupID: 2, Model: "model-a", At: now.Add(-time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Failure: 8}},
		{GroupID: 1, Model: "model-a-discount", At: now.Add(-time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Failure: 99}},
	}}
	s := NewChannelPerformanceService(r)
	scope := []ChannelPerformanceScope{{Key: "card", Platform: "openai", Model: "model-a", Groups: map[int64]string{1: "visible"}}}
	result, err := s.Query(context.Background(), f, scope, true)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, r.requested)
	require.Len(t, result.Items, 1)
	require.Len(t, result.Items[0].Groups, 1)
	require.Equal(t, 100.0, *result.Items[0].SuccessRate)
	require.Len(t, result.Items[0].Trend, 24)
	require.Nil(t, result.Items[0].Trend[0].SuccessRate)
	_, err = s.Query(context.Background(), f, scope, true)
	require.NoError(t, err)
	require.Equal(t, 1, r.calls)
	restricted := []ChannelPerformanceScope{{Key: "card", Platform: "openai", Model: "model-a", Groups: map[int64]string{2: "other"}}}
	result, err = s.Query(context.Background(), f, restricted, true)
	require.NoError(t, err)
	require.Equal(t, 2, r.calls)
	require.Equal(t, 0.0, *result.Items[0].SuccessRate)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, private := range []string{"OutputTokens", "Success\"", "Failure\"", "account", "api_key", "user_id", "sample_count"} {
		require.NotContains(t, string(encoded), private)
	}
	f.GroupID = 2
	result, err = s.Query(context.Background(), f, scope, true)
	require.NoError(t, err)
	require.Empty(t, result.Items)
}

func TestPerformanceWindowsAndEmptyBuckets(t *testing.T) {
	for name, points := range map[string]int{"90m": 19, "24h": 25, "7d": 28, "30d": 31} {
		t.Run(name, func(t *testing.T) {
			f := ChannelPerformanceFilter{Range: name}
			require.NoError(t, f.Normalize(time.Date(2026, 9, 14, 12, 23, 45, 0, time.UTC)))
			trend := performanceTimeline(f, nil)
			require.Len(t, trend, points)
			require.LessOrEqual(t, len(trend), 90)
			for _, p := range trend {
				require.Nil(t, p.SuccessRate)
				require.Nil(t, p.OutputTPS)
			}
		})
	}
	f := ChannelPerformanceFilter{Range: "1y"}
	require.Error(t, f.Normalize(time.Now()))
}

func TestPerformanceCoverageUsesVisibleModelSamples(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	f := ChannelPerformanceFilter{Range: "24h"}
	require.NoError(t, f.Normalize(now))
	r := &performanceRepoStub{rows: []ChannelPerformanceRow{
		{GroupID: 2, Model: "visible", At: now.Add(-20 * time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Success: 10}},
		{GroupID: 1, Model: "hidden-model", At: now.Add(-10 * time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Success: 10}},
		{GroupID: 1, Model: "visible", At: now.Add(-time.Hour), ChannelPerformanceCounts: ChannelPerformanceCounts{Success: 2, CharacterCount: 1, CharacterSum: 100}},
	}}
	s := NewChannelPerformanceService(r)
	scope := []ChannelPerformanceScope{{Key: "card", Model: "visible", Groups: map[int64]string{1: "visible-group"}}}
	result, err := s.Query(context.Background(), f, scope, true)
	require.NoError(t, err)
	require.NotNil(t, result.CoverageStart)
	require.Equal(t, now.Add(-time.Hour), *result.CoverageStart)
	require.Equal(t, now, *result.CoverageEnd)
	require.Equal(t, "low", result.Items[0].SampleQuality)
	require.InDelta(t, 100, *result.Items[0].FirstCharacterMs, 0.001)
}
