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

func TestIntelligenceProbeSVGRegex(t *testing.T) {
	cases := []struct {
		name    string
		reply   string
		wantSVG bool
	}{
		{
			name:    "plain svg",
			reply:   `<svg xmlns="http://www.w3.org/2000/svg" width="10"><circle r="5"/></svg>`,
			wantSVG: true,
		},
		{
			name:    "svg wrapped in prose and fences",
			reply:   "Here is your drawing:\n```svg\n<svg viewBox=\"0 0 10 10\">\n  <path d=\"M0 0\"/>\n</svg>\n```",
			wantSVG: true,
		},
		{
			name:    "no svg",
			reply:   "I cannot draw that.",
			wantSVG: false,
		},
		{
			name:    "unclosed svg is not a document",
			reply:   `<svg><circle r="5"/>`,
			wantSVG: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := intelligenceProbeSVGRegex.FindString(tc.reply)
			if (got != "") != tc.wantSVG {
				t.Fatalf("match = %q, wantSVG = %v", got, tc.wantSVG)
			}
			if tc.wantSVG && !strings.HasPrefix(strings.ToLower(got), "<svg") {
				t.Fatalf("match must start with <svg, got %q", got)
			}
		})
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
