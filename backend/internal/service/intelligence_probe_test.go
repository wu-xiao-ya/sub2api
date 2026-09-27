package service

import (
	"strings"
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
