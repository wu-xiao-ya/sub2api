package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNormalizeIntelligenceProbeSettings(t *testing.T) {
	tests := []struct {
		name               string
		in                 IntelligenceProbeSettings
		wantInterval       int
		wantRetention      int
		wantPromptOverride string
	}{
		{
			name:               "defaults pass through",
			in:                 IntelligenceProbeSettings{IntervalMinutes: 12, RetentionDays: 1},
			wantInterval:       12,
			wantRetention:      1,
			wantPromptOverride: "",
		},
		{
			name:               "interval clamped low",
			in:                 IntelligenceProbeSettings{IntervalMinutes: 0, RetentionDays: 1},
			wantInterval:       IntelligenceProbeMinIntervalMinutes,
			wantRetention:      1,
			wantPromptOverride: "",
		},
		{
			name:               "interval clamped high",
			in:                 IntelligenceProbeSettings{IntervalMinutes: 100000, RetentionDays: 1},
			wantInterval:       IntelligenceProbeMaxIntervalMinutes,
			wantRetention:      1,
			wantPromptOverride: "",
		},
		{
			name:               "retention clamped",
			in:                 IntelligenceProbeSettings{IntervalMinutes: 12, RetentionDays: 999},
			wantInterval:       12,
			wantRetention:      IntelligenceProbeMaxRetentionDays,
			wantPromptOverride: "",
		},
		{
			name:               "prompt override trimmed",
			in:                 IntelligenceProbeSettings{IntervalMinutes: 12, RetentionDays: 1, PromptOverride: "  draw  "},
			wantInterval:       12,
			wantRetention:      1,
			wantPromptOverride: "draw",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := tc.in
			normalizeIntelligenceProbeSettings(&settings)
			if settings.IntervalMinutes != tc.wantInterval {
				t.Fatalf("interval = %d, want %d", settings.IntervalMinutes, tc.wantInterval)
			}
			if settings.RetentionDays != tc.wantRetention {
				t.Fatalf("retention = %d, want %d", settings.RetentionDays, tc.wantRetention)
			}
			if settings.PromptOverride != tc.wantPromptOverride {
				t.Fatalf("prompt override = %q, want %q", settings.PromptOverride, tc.wantPromptOverride)
			}
		})
	}
}

func TestIntelligenceProbeSettingsPrompt(t *testing.T) {
	custom := IntelligenceProbeSettings{PromptOverride: "my custom pelican"}
	if custom.prompt() != "my custom pelican" {
		t.Fatalf("prompt = %q, want custom override", custom.prompt())
	}
	if !strings.Contains(intelligenceProbeDefaultPrompt, "pelican") {
		t.Fatalf("default prompt %q must mention the pelican", intelligenceProbeDefaultPrompt)
	}
}

func TestExtractBalancedSVGWrappedInProse(t *testing.T) {
	reply := "Here is your drawing:\n```svg\n<svg viewBox=\"0 0 10 10\">\n  <path d=\"M0 0\"/>\n</svg>\n```"
	got := ExtractBalancedSVG(reply)
	if !strings.HasPrefix(strings.ToLower(got), "<svg") {
		t.Fatalf("wrapped svg not extracted: %q", got)
	}
	if got := ExtractBalancedSVG("I cannot draw that."); got != "" {
		t.Fatalf("reply without svg = %q, want empty", got)
	}
}

func TestTruncateProbeMessage(t *testing.T) {
	if got := truncateProbeMessage("  short  "); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("x", intelligenceProbeExcerptBytes+50)
	got := truncateProbeMessage(long)
	if len(got) != intelligenceProbeExcerptBytes {
		t.Fatalf("len = %d, want %d", len(got), intelligenceProbeExcerptBytes)
	}
}

func TestExtractBalancedSVGPicksLongestBalancedBlock(t *testing.T) {
	// Multi-document reply (animation frames): the naive greedy match would
	// splice both documents into invalid XML.
	reply := "<p>here</p>" +
		`<svg viewBox="0 0 10 10"><rect width="2" height="2"/></svg>` +
		"middle text" +
		`<svg viewBox="0 0 100 100"><rect width="50" height="50"/><circle r="20"/></svg>`
	got := ExtractBalancedSVG(reply)
	want := `<svg viewBox="0 0 100 100"><rect width="50" height="50"/><circle r="20"/></svg>`
	if got != want {
		t.Fatalf("got %q, want the longest balanced block", got)
	}
}

func TestExtractBalancedSVGHandlesNestingAndComments(t *testing.T) {
	// Nested svg elements plus a commented-out frame before the real drawing.
	reply := "<!-- draft: <svg><rect/></svg> -->" +
		`<svg><g><svg id="inner"><circle r="3"/></svg><rect width="9"/></g></svg>`
	got := ExtractBalancedSVG(reply)
	if got != `<svg><g><svg id="inner"><circle r="3"/></svg><rect width="9"/></g></svg>` {
		t.Fatalf("nested structure mangled: %q", got)
	}
}

func TestExtractBalancedSVGRejectsTruncated(t *testing.T) {
	if got := ExtractBalancedSVG(`<svg><rect width="3"/>`); got != "" {
		t.Fatalf("truncated svg = %q, want empty", got)
	}
}

func TestExtractBalancedSVGSelfClosingFrame(t *testing.T) {
	reply := `<svg><defs><svg/><rect width="4"/></defs><rect width="9"/></svg>`
	got := ExtractBalancedSVG(reply)
	if got != reply {
		t.Fatalf("self-closing nested frame mangled: %q", got)
	}
}

func TestEnsureSVGNamespace(t *testing.T) {
	const xmlns = ` xmlns="http://www.w3.org/2000/svg"`
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing namespace appended to root tag",
			in:   `<svg viewBox="0 0 9 9"><rect/></svg>`,
			want: `<svg viewBox="0 0 9 9"` + xmlns + `><rect/></svg>`,
		},
		{
			name: "existing namespace kept",
			in:   `<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`,
			want: `<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`,
		},
		{
			name: "xlink prefix declaration does not count",
			in:   `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><rect/></svg>`,
			want: `<svg xmlns:xlink="http://www.w3.org/1999/xlink"` + xmlns + `><rect/></svg>`,
		},
		{
			name: "self-closing root",
			in:   `<svg viewBox="0 0 1 1"/>`,
			want: `<svg viewBox="0 0 1 1"` + xmlns + `/>`,
		},
		{
			name: "bare root",
			in:   `<svg><rect/></svg>`,
			want: `<svg` + xmlns + `><rect/></svg>`,
		},
		{
			name: "non-svg untouched",
			in:   "no drawing here",
			want: "no drawing here",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ensureSVGNamespace(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRetryableProbeFailure(t *testing.T) {
	tests := []struct {
		name       string
		httpStatus int
		err        error
		text       string
		want       bool
	}{
		{"503 retried", 503, probeErr("upstream error"), "", true},
		{"502 retried", 502, probeErr("upstream error"), "", true},
		{"400 not retried", 400, probeErr("bad request"), "", false},
		{"transport error retried", 0, probeErr("timeout"), "", true},
		{"mid-stream read error not retried", 200, probeErr("canceled"), "partial text", false},
		{"empty 2xx stream retried", 200, nil, "", true},
		{"prose answer not retried", 200, nil, "I cannot draw.", false},
		{"success not retried", 200, nil, "<svg></svg>", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableProbeFailure(tc.httpStatus, tc.err, tc.text); got != tc.want {
				t.Fatalf("retryableProbeFailure(%d, _, %q) = %v, want %v", tc.httpStatus, tc.text, got, tc.want)
			}
		})
	}
}

// probeErr keeps the tables above readable: retryableProbeFailure and the
// message builder only check err against nil, so any non-nil error works.
func probeErr(message string) error {
	return errors.New(message)
}

func sseUpstream(handler func(attempt int32, w http.ResponseWriter)) *httptest.Server {
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(atomic.AddInt32(&calls, 1), w)
	}))
}

func writeProbeSSE(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	payload := fmt.Sprintf(`{"choices":[{"delta":{"content":%s}}]}`, strconv.Quote(content))
	_, _ = w.Write([]byte("data: " + payload + "\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

func probeTestService(gatewayURL string) *IntelligenceProbeService {
	svc := NewIntelligenceProbeService(nil, nil, nil)
	svc.gatewayURL = gatewayURL
	return svc
}

func TestRunProbeAttemptsRetriesTransientFailure(t *testing.T) {
	upstream := sseUpstream(func(attempt int32, w http.ResponseWriter) {
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"Service temporarily unavailable"}}`))
			return
		}
		writeProbeSSE(w, `<svg xmlns="http://www.w3.org/2000/svg"><rect width="2"/></svg>`)
	})
	defer upstream.Close()

	result := probeTestService(upstream.URL).runProbeAttempts(context.Background(),
		&IntelligenceProbeTarget{ID: 7, GroupID: 1, Model: "gpt-6-astra"}, "key", "draw")
	if result.Status != intelligenceProbeStatusSuccess {
		t.Fatalf("status = %q (%s), want success", result.Status, result.ErrorMessage)
	}
	if !strings.HasPrefix(result.ResponseSVG, `<svg xmlns=`) {
		t.Fatalf("response svg lost its namespace: %q", result.ResponseSVG)
	}
}

func TestRunProbeAttemptsEmptyStreamRetriedThenNoSVG(t *testing.T) {
	upstream := sseUpstream(func(_ int32, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	defer upstream.Close()

	result := probeTestService(upstream.URL).runProbeAttempts(context.Background(),
		&IntelligenceProbeTarget{ID: 8, GroupID: 1, Model: "gpt-6-astra"}, "key", "draw")
	if result.Status != intelligenceProbeStatusNoSVG {
		t.Fatalf("status = %q, want no_svg", result.Status)
	}
	if !strings.Contains(result.ResponseExcerpt, "no content") {
		t.Fatalf("empty-stream explanation missing, excerpt = %q", result.ResponseExcerpt)
	}
}

func TestRunProbeAttemptsPersistentFailureNotRetried(t *testing.T) {
	upstream := sseUpstream(func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Upstream access forbidden"}}`))
	})
	defer upstream.Close()

	result := probeTestService(upstream.URL).runProbeAttempts(context.Background(),
		&IntelligenceProbeTarget{ID: 9, GroupID: 1, Model: "gpt-6-astra"}, "key", "draw")
	if result.Status != intelligenceProbeStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.ErrorMessage, "HTTP 403") {
		t.Fatalf("error message = %q, want HTTP 403 detail", result.ErrorMessage)
	}
	if strings.Contains(result.ErrorMessage, "attempt1") {
		t.Fatalf("4xx must not retry, message = %q", result.ErrorMessage)
	}
}
