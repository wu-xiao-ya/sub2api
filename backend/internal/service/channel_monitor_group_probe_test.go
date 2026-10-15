//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type groupProbeRepoStub struct {
	ChannelMonitorRepository
	monitors map[int64]*ChannelMonitor
	rows     []*ChannelMonitorHistoryRow
	marked   []int64
}

func (r *groupProbeRepoStub) GetByID(_ context.Context, id int64) (*ChannelMonitor, error) {
	monitor := r.monitors[id]
	if monitor == nil {
		return nil, errors.New("monitor not found")
	}
	cloned := *monitor
	return &cloned, nil
}

func (r *groupProbeRepoStub) InsertHistoryBatch(_ context.Context, rows []*ChannelMonitorHistoryRow) error {
	r.rows = append(r.rows, rows...)
	return nil
}

func (r *groupProbeRepoStub) MarkChecked(_ context.Context, id int64, _ time.Time) error {
	r.marked = append(r.marked, id)
	return nil
}

type groupProbeEncryptor struct{}

func (groupProbeEncryptor) Encrypt(plaintext string) (string, error) { return plaintext, nil }
func (groupProbeEncryptor) Decrypt(ciphertext string) (string, error) {
	return ciphertext, nil
}

func TestRunGroupProbeChecksRunsCandidatesConcurrently(t *testing.T) {
	swapMonitorHTTPClient(t)

	started := make(chan struct{}, monitorGroupProbeMaxCandidates)
	release := make(chan struct{})
	var mu sync.Mutex
	var bodies []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		defer func() { _ = r.Body.Close() }()
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		started <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": "1"},
			}},
		})
	}))
	t.Cleanup(server.Close)

	monitors := make([]*ChannelMonitor, 0, monitorGroupProbeMaxCandidates)
	for i := 0; i < monitorGroupProbeMaxCandidates; i++ {
		monitors = append(monitors, &ChannelMonitor{
			ID:           int64(i + 1),
			Name:         "plus-line",
			Provider:     MonitorProviderOpenAI,
			APIMode:      MonitorAPIModeChatCompletions,
			Endpoint:     server.URL,
			APIKey:       "sk-test",
			PrimaryModel: "gpt-test",
			Enabled:      true,
			GroupName:    "Plus",
		})
	}

	svc := NewChannelMonitorService(nil, nil)
	done := make(chan []groupProbeResult, 1)
	go func() {
		done <- svc.runGroupProbeChecks(context.Background(), monitors)
	}()

	for i := 0; i < monitorGroupProbeMaxCandidates; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("candidate %d did not start before earlier requests completed", i+1)
		}
	}
	close(release)

	var probes []groupProbeResult
	select {
	case probes = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("group probes did not complete")
	}
	if len(probes) != monitorGroupProbeMaxCandidates {
		t.Fatalf("got %d probe results, want %d", len(probes), monitorGroupProbeMaxCandidates)
	}
	for _, probe := range probes {
		if probe.result == nil || probe.result.Status != MonitorStatusOperational {
			t.Fatalf("expected operational group result, got %#v", probe.result)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != monitorGroupProbeMaxCandidates {
		t.Fatalf("got %d request bodies, want %d", len(bodies), monitorGroupProbeMaxCandidates)
	}
	for _, body := range bodies {
		if body["max_tokens"] != float64(monitorLowCostMaxTokens) {
			t.Fatalf("low-cost group request should cap max_tokens, got %#v", body["max_tokens"])
		}
	}
}

func TestSelectBestGroupProbePrefersHealthyThenLowestLatency(t *testing.T) {
	slowLatency := 900
	fastLatency := 120
	failedLatency := 30

	summary := selectBestGroupProbe([]groupProbeResult{
		{
			monitor: &ChannelMonitor{ID: 1, Name: "line-1", GroupName: "Plus"},
			result:  &CheckResult{Status: MonitorStatusOperational, LatencyMs: &slowLatency},
		},
		{
			monitor: &ChannelMonitor{ID: 2, Name: "line-2", GroupName: "Plus"},
			result:  &CheckResult{Status: MonitorStatusOperational, LatencyMs: &fastLatency},
		},
		{
			monitor: &ChannelMonitor{ID: 3, Name: "line-3", GroupName: "Plus"},
			result:  &CheckResult{Status: MonitorStatusFailed, LatencyMs: &failedLatency},
		},
	})

	if summary == nil {
		t.Fatal("expected group summary")
	}
	if summary.BestMonitorID != 2 {
		t.Fatalf("best monitor = %d, want fastest healthy line 2", summary.BestMonitorID)
	}
	if summary.SuccessfulCount != 2 {
		t.Fatalf("successful count = %d, want 2", summary.SuccessfulCount)
	}
}

func TestRunGroupCheckPersistsEveryLineAndReturnsBest(t *testing.T) {
	swapMonitorHTTPClient(t)

	newServer := func(delay time.Duration) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]string{"content": "1"},
				}},
			})
		}))
	}

	slow := newServer(35 * time.Millisecond)
	fast := newServer(1 * time.Millisecond)
	t.Cleanup(slow.Close)
	t.Cleanup(fast.Close)

	repo := &groupProbeRepoStub{
		monitors: map[int64]*ChannelMonitor{
			1: {
				ID:           1,
				Name:         "plus-1",
				Provider:     MonitorProviderOpenAI,
				APIMode:      MonitorAPIModeChatCompletions,
				Endpoint:     slow.URL,
				APIKey:       "sk-slow",
				PrimaryModel: "gpt-test",
				GroupName:    "Plus",
				Enabled:      true,
			},
			2: {
				ID:           2,
				Name:         "plus-2",
				Provider:     MonitorProviderOpenAI,
				APIMode:      MonitorAPIModeChatCompletions,
				Endpoint:     fast.URL,
				APIKey:       "sk-fast",
				PrimaryModel: "gpt-test",
				GroupName:    "Plus",
				Enabled:      true,
			},
		},
	}

	svc := NewChannelMonitorService(repo, groupProbeEncryptor{})
	summary, err := svc.RunGroupCheck(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("RunGroupCheck returned error: %v", err)
	}
	if summary == nil {
		t.Fatal("expected grouped check summary")
	}
	if summary.BestMonitorID != 2 {
		t.Fatalf("best monitor = %d, want fast line 2", summary.BestMonitorID)
	}
	if len(repo.rows) != 2 || len(repo.marked) != 2 {
		t.Fatalf("expected one persisted primary result per line, rows=%d marked=%d", len(repo.rows), len(repo.marked))
	}
	for _, row := range repo.rows {
		if row.Status != MonitorStatusOperational || row.Model != "gpt-test" {
			t.Fatalf("unexpected persisted result: %#v", row)
		}
	}
}

func TestSelectBestMonitorForUserViewUsesHealthyFastestLine(t *testing.T) {
	slowLatency := 700
	fastLatency := 110
	monitors := []*ChannelMonitor{
		{ID: 1, Name: "plus-1", GroupName: "Plus", Enabled: true},
		{ID: 2, Name: "plus-2", GroupName: "Plus", Enabled: true},
		{ID: 3, Name: "plus-3", GroupName: "Plus", Enabled: true},
	}
	best := selectBestMonitorForUserView(monitors, map[int64]MonitorStatusSummary{
		1: {PrimaryStatus: MonitorStatusError},
		2: {PrimaryStatus: MonitorStatusOperational, PrimaryLatencyMs: &slowLatency},
		3: {PrimaryStatus: MonitorStatusOperational, PrimaryLatencyMs: &fastLatency},
	})
	if best == nil || best.ID != 3 {
		t.Fatalf("best public monitor = %#v, want fastest healthy line 3", best)
	}
}
