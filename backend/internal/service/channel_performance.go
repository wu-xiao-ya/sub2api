package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"golang.org/x/sync/singleflight"
	"sort"
	"sync"
	"time"
)

const ChannelPerformanceVersion = 2

type ChannelPerformanceFilter struct {
	Range           string
	Model           string
	GroupID         int64
	ServiceTier     string
	ReasoningEffort string
	Stream          *bool
	Start           time.Time
	End             time.Time
	Bucket          time.Duration
}

func (f *ChannelPerformanceFilter) Normalize(now time.Time) error {
	var window time.Duration
	switch f.Range {
	case "90m":
		window = 90 * time.Minute
		f.Bucket = 5 * time.Minute
	case "", "24h":
		f.Range = "24h"
		window = 24 * time.Hour
		f.Bucket = time.Hour
	case "7d":
		window = 7 * 24 * time.Hour
		f.Bucket = 6 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
		f.Bucket = 24 * time.Hour
	default:
		return fmt.Errorf("invalid range")
	}
	f.End = now.UTC().Truncate(time.Minute)
	if f.Range == "7d" || f.Range == "30d" {
		f.End = f.End.Truncate(time.Hour)
	}
	f.Start = f.End.Add(-window)
	if len(f.Model) > 256 || len(f.ServiceTier) > 64 || len(f.ReasoningEffort) > 64 {
		return fmt.Errorf("filter is too long")
	}
	return nil
}

// Scope comes only from server-side available-channel permission filtering.
type ChannelPerformanceScope struct {
	Key      string
	Platform string
	Model    string
	Groups   map[int64]string
}

type ChannelPerformanceCounts struct {
	Success        int64
	Failure        int64
	Unknown        int64
	CharacterCount int64
	CharacterSum   float64
	DurationCount  int64
	DurationSum    float64
	OutputTokens   int64
	OutputCount    int64
	OutputMs       float64
}

func (c *ChannelPerformanceCounts) Add(v ChannelPerformanceCounts) {
	c.Success += v.Success
	c.Failure += v.Failure
	c.Unknown += v.Unknown
	c.CharacterCount += v.CharacterCount
	c.CharacterSum += v.CharacterSum
	c.DurationCount += v.DurationCount
	c.DurationSum += v.DurationSum
	c.OutputTokens += v.OutputTokens
	c.OutputCount += v.OutputCount
	c.OutputMs += v.OutputMs
}

type ChannelPerformanceMetric struct {
	FirstCharacterMs *float64 `json:"first_character_ms"`
	DurationMs       *float64 `json:"duration_ms"`
	OutputTPS        *float64 `json:"output_tps"`
	SuccessRate      *float64 `json:"success_rate"`
	SampleQuality    string   `json:"sample_quality"`
	CoverageStatus   string   `json:"coverage_status"`
}

func (c ChannelPerformanceCounts) Metric() ChannelPerformanceMetric {
	m := ChannelPerformanceMetric{SampleQuality: "empty", CoverageStatus: "complete"}
	if c.Unknown > 0 {
		m.CoverageStatus = "partial"
	}
	ratio := func(sum float64, n int64) *float64 {
		if n <= 0 {
			return nil
		}
		v := sum / float64(n)
		return &v
	}
	m.FirstCharacterMs = ratio(c.CharacterSum, c.CharacterCount)
	m.DurationMs = ratio(c.DurationSum, c.DurationCount)
	if c.OutputMs > 0 && c.OutputTokens > 0 {
		v := float64(c.OutputTokens) * 1000 / c.OutputMs
		m.OutputTPS = &v
	}
	total := c.Success + c.Failure
	if total > 0 {
		rate := float64(c.Success) * 100 / float64(total)
		m.SuccessRate = &rate
		m.SampleQuality = "low"
		if total >= 10 {
			m.SampleQuality = "adequate"
		}
		for _, samples := range []int64{c.CharacterCount, c.DurationCount, c.OutputCount} {
			if samples > 0 && samples < 10 {
				m.SampleQuality = "low"
			}
		}
	}
	if total == 0 && c.Unknown == 0 {
		m.CoverageStatus = "empty"
	}
	return m
}

type ChannelPerformancePoint struct {
	At time.Time `json:"at"`
	ChannelPerformanceMetric
}
type ChannelPerformanceGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	ChannelPerformanceMetric
	Trend []ChannelPerformancePoint `json:"trend"`
}
type ChannelPerformanceItem struct {
	Key      string `json:"key"`
	Model    string `json:"model"`
	Platform string `json:"platform"`
	ChannelPerformanceMetric
	Groups []ChannelPerformanceGroup `json:"groups,omitempty"`
	Trend  []ChannelPerformancePoint `json:"trend,omitempty"`
}
type ChannelPerformanceResult struct {
	Items           []ChannelPerformanceItem `json:"items"`
	Version         int                      `json:"version"`
	Source          string                   `json:"source"`
	Start           time.Time                `json:"start"`
	End             time.Time                `json:"end"`
	UpdatedAt       *time.Time               `json:"updated_at"`
	CoverageStart   *time.Time               `json:"coverage_start"`
	CoverageEnd     *time.Time               `json:"coverage_end"`
	IncompleteSince *time.Time               `json:"incomplete_since"`
}
type ChannelPerformanceRow struct {
	GroupID int64
	Model   string
	At      time.Time
	ChannelPerformanceCounts
}
type ChannelPerformanceCoverage struct{ Start, End, UpdatedAt, IncompleteSince *time.Time }
type ChannelPerformanceRepository interface {
	Query(context.Context, ChannelPerformanceFilter, []int64) ([]ChannelPerformanceRow, ChannelPerformanceCoverage, error)
	Recompute(context.Context, time.Time, time.Time) error
	Coverage(context.Context) (ChannelPerformanceCoverage, error)
}

type performanceCacheEntry struct {
	value   *ChannelPerformanceResult
	expires time.Time
}
type ChannelPerformanceService struct {
	repo    ChannelPerformanceRepository
	mu      sync.Mutex
	cache   map[string]performanceCacheEntry
	flight  singleflight.Group
	runtime *channelPerformanceRuntime
}

func NewChannelPerformanceService(repo ChannelPerformanceRepository) *ChannelPerformanceService {
	return &ChannelPerformanceService{repo: repo, cache: make(map[string]performanceCacheEntry)}
}

func (s *ChannelPerformanceService) Query(ctx context.Context, f ChannelPerformanceFilter, scopes []ChannelPerformanceScope, detail bool) (*ChannelPerformanceResult, error) {
	encoded, _ := json.Marshal(struct {
		F ChannelPerformanceFilter
		S []ChannelPerformanceScope
		D bool
	}{f, scopes, detail})
	key := string(encoded)
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.value, nil
	}
	ch := s.flight.DoChan(key, func() (any, error) {
		queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		result, err := s.query(queryCtx, f, scopes, detail)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.cache) >= 128 {
			s.cache = make(map[string]performanceCacheEntry)
		}
		s.cache[key] = performanceCacheEntry{result, time.Now().Add(5 * time.Second)}
		return result, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-ch:
		if v.Err != nil {
			return nil, v.Err
		}
		return v.Val.(*ChannelPerformanceResult), nil
	}
}

func (s *ChannelPerformanceService) query(ctx context.Context, f ChannelPerformanceFilter, scopes []ChannelPerformanceScope, detail bool) (*ChannelPerformanceResult, error) {
	result := &ChannelPerformanceResult{Version: ChannelPerformanceVersion, Source: "user_requests", Start: f.Start, End: f.End, Items: []ChannelPerformanceItem{}}
	ids := map[int64]bool{}
	for _, scope := range scopes {
		for id := range scope.Groups {
			if f.GroupID == 0 || f.GroupID == id {
				ids[id] = true
			}
		}
	}
	groupIDs := make([]int64, 0, len(ids))
	for id := range ids {
		groupIDs = append(groupIDs, id)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	if len(groupIDs) == 0 {
		return result, nil
	}
	rows, coverage, err := s.repo.Query(ctx, f, groupIDs)
	if err != nil {
		return nil, err
	}
	result.UpdatedAt = coverage.UpdatedAt
	result.IncompleteSince = coverage.IncompleteSince
	if s.runtime != nil && s.runtime.gap.Load() != 0 {
		gap := time.Unix(s.runtime.gap.Load(), 0).UTC()
		if result.IncompleteSince == nil || gap.Before(*result.IncompleteSince) {
			result.IncompleteSince = &gap
		}
	}
	for _, scope := range scopes {
		if f.Model != "" && !performanceModelsEqual(scope.Model, f.Model) {
			continue
		}
		groups := map[int64]ChannelPerformanceCounts{}
		timeline := map[time.Time]ChannelPerformanceCounts{}
		byGroup := map[int64]map[time.Time]ChannelPerformanceCounts{}
		var total ChannelPerformanceCounts
		for _, row := range rows {
			if _, ok := scope.Groups[row.GroupID]; !ok || !ids[row.GroupID] || !performanceModelsEqual(row.Model, scope.Model) {
				continue
			}
			if row.At.Before(f.Start.Truncate(f.Bucket)) || !row.At.Before(f.End) {
				continue
			}
			start := row.At
			if start.Before(f.Start) {
				start = f.Start
			}
			end := row.At.Add(f.Bucket)
			if end.After(f.End) {
				end = f.End
			}
			if result.CoverageStart == nil || start.Before(*result.CoverageStart) {
				result.CoverageStart = &start
			}
			if result.CoverageEnd == nil || end.After(*result.CoverageEnd) {
				result.CoverageEnd = &end
			}
			row.At = row.At.UTC().Truncate(f.Bucket)
			total.Add(row.ChannelPerformanceCounts)
			group := groups[row.GroupID]
			group.Add(row.ChannelPerformanceCounts)
			groups[row.GroupID] = group
			count := timeline[row.At]
			count.Add(row.ChannelPerformanceCounts)
			timeline[row.At] = count
			if byGroup[row.GroupID] == nil {
				byGroup[row.GroupID] = map[time.Time]ChannelPerformanceCounts{}
			}
			count = byGroup[row.GroupID][row.At]
			count.Add(row.ChannelPerformanceCounts)
			byGroup[row.GroupID][row.At] = count
		}
		item := ChannelPerformanceItem{Key: scope.Key, Model: scope.Model, Platform: scope.Platform, ChannelPerformanceMetric: total.Metric()}
		if detail {
			item.Trend = performanceTimeline(f, timeline)
			for _, id := range groupIDs {
				if name, ok := scope.Groups[id]; ok {
					item.Groups = append(item.Groups, ChannelPerformanceGroup{ID: id, Name: name, ChannelPerformanceMetric: groups[id].Metric(), Trend: performanceTimeline(f, byGroup[id])})
				}
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func performanceTimeline(f ChannelPerformanceFilter, values map[time.Time]ChannelPerformanceCounts) []ChannelPerformancePoint {
	points := []ChannelPerformancePoint{}
	for at := f.Start.Truncate(f.Bucket); at.Before(f.End) && len(points) < 90; at = at.Add(f.Bucket) {
		points = append(points, ChannelPerformancePoint{At: at, ChannelPerformanceMetric: values[at].Metric()})
	}
	return points
}

func PerformanceModelNames(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return []string{}
	}
	names := []string{model}
	identity := performanceModelIdentity(model)
	if identity != "" && !strings.EqualFold(identity, model) {
		names = append(names, identity)
	}
	if hyphen := grokDottedVersionToHyphen(identity); hyphen != "" && !containsFold(names, hyphen) {
		names = append(names, hyphen)
	}
	return names
}

func performanceModelsEqual(stored, card string) bool {
	return strings.EqualFold(strings.TrimSpace(stored), strings.TrimSpace(card)) ||
		performanceModelIdentity(stored) == performanceModelIdentity(card)
}

func performanceModelIdentity(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	stripped := strings.ToLower(xai.StripGrokProviderPrefix(trimmed))
	if !xai.IsGrokModelID(stripped) {
		return trimmed
	}
	for _, candidate := range []string{stripped, grokHyphenVersionToDotted(stripped)} {
		if canonical := xai.ResolveGrokTextResponsesModelID(candidate); xai.IsGrokTextResponsesModelID(canonical) {
			return canonical
		}
	}
	return trimmed
}

func grokHyphenVersionToDotted(model string) string {
	parts := strings.Split(model, "-")
	if len(parts) < 3 || parts[0] != "grok" || !digitsOnly(parts[1]) || !digitsOnly(parts[2]) {
		return model
	}
	out := parts[0] + "-" + parts[1] + "." + parts[2]
	if len(parts) > 3 {
		out += "-" + strings.Join(parts[3:], "-")
	}
	return out
}

func grokDottedVersionToHyphen(model string) string {
	parts := strings.SplitN(model, ".", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "grok-") {
		return ""
	}
	head := strings.TrimPrefix(parts[0], "grok-")
	rest := parts[1]
	version, suffix, _ := strings.Cut(rest, "-")
	if !digitsOnly(head) || !digitsOnly(version) {
		return ""
	}
	out := "grok-" + head + "-" + version
	if suffix != "" {
		out += "-" + suffix
	}
	return out
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
