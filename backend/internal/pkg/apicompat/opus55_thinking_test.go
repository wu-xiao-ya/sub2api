package apicompat

import (
	"encoding/json"
	"testing"
)

func anthropicThinkingFromRequest(t *testing.T, req *AnthropicRequest) (thinkingType, budgetSeen string, effort string) {
	t.Helper()
	if req.Thinking != nil {
		raw, err := json.Marshal(req.Thinking)
		if err != nil {
			t.Fatalf("marshal thinking: %v", err)
		}
		var shaped struct {
			Type         string `json:"type"`
			BudgetTokens *int   `json:"budget_tokens"`
		}
		if err := json.Unmarshal(raw, &shaped); err != nil {
			t.Fatalf("unmarshal thinking: %v", err)
		}
		if shaped.BudgetTokens != nil {
			budgetSeen = "present"
		}
		thinkingType = shaped.Type
	}
	if req.OutputConfig != nil {
		effort = req.OutputConfig.Effort
	}
	return
}

func TestResponsesToAnthropicOpus55NormalizesThinking(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-5-5",
		Input: json.RawMessage(`"hi"`),
		Reasoning: &ResponsesReasoning{
			Effort: "high",
		},
	}
	out, err := ResponsesToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	thinkingType, budgetSeen, effort := anthropicThinkingFromRequest(t, out)
	if thinkingType != "adaptive" {
		t.Fatalf("thinking.type = %q, want adaptive", thinkingType)
	}
	if budgetSeen != "" {
		t.Fatal("budget_tokens must be dropped for opus-5-5")
	}
	if effort != "high" {
		t.Fatalf("effort = %q, want high", effort)
	}
}

func TestResponsesToAnthropicOpus55PairsLoneEffort(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-5-5",
		Input: json.RawMessage(`"hi"`),
		Reasoning: &ResponsesReasoning{
			Effort: "low",
		},
	}
	out, err := ResponsesToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	thinkingType, _, effort := anthropicThinkingFromRequest(t, out)
	if thinkingType != "adaptive" {
		t.Fatalf("thinking.type = %q, want adaptive paired with effort", thinkingType)
	}
	if effort != "low" {
		t.Fatalf("effort = %q, want low", effort)
	}
}

func TestResponsesToAnthropicNonOpusKeepsLegacyShape(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-sonnet-5",
		Input: json.RawMessage(`"hi"`),
		Reasoning: &ResponsesReasoning{
			Effort: "high",
		},
	}
	out, err := ResponsesToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	thinkingType, budgetSeen, _ := anthropicThinkingFromRequest(t, out)
	if thinkingType != "enabled" || budgetSeen == "" {
		t.Fatalf("non-opus thinking = %q/%q, want legacy enabled+budget", thinkingType, budgetSeen)
	}
}

func TestResponsesToAnthropicOpus55DerivedEffortFromBudget(t *testing.T) {
	// effort=xhigh maps to max; the normalized request must carry effort=max
	// and no budget_tokens.
	req := &ResponsesRequest{
		Model: "claude-opus-5-5",
		Input: json.RawMessage(`"hi"`),
		Reasoning: &ResponsesReasoning{
			Effort: "xhigh",
		},
	}
	out, err := ResponsesToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	thinkingType, budgetSeen, effort := anthropicThinkingFromRequest(t, out)
	if thinkingType != "adaptive" || budgetSeen != "" || effort != "max" {
		t.Fatalf("thinking=%q budget=%q effort=%q, want adaptive///max", thinkingType, budgetSeen, effort)
	}
}
