package eval

import (
	"context"
	"strings"
	"testing"
)

func TestRequiredToolsPassesWhenAllNamesAppear(t *testing.T) {
	result, err := (RequiredTools{Names: []string{"search", "plan_route"}}).Score(context.Background(), nil, caseWithCalls(
		[]ToolCall{{Name: "search"}, {Name: "plan_route"}},
		nil,
	))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected pass, got %+v", result)
	}
}

func TestRequiredToolsFailsWhenNameMissing(t *testing.T) {
	result, err := (RequiredTools{Names: []string{"search", "plan_route"}}).Score(context.Background(), nil, caseWithCalls(
		[]ToolCall{{Name: "search"}},
		nil,
	))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0 || !strings.Contains(result.Reason, "plan_route") {
		t.Fatalf("expected missing tool failure, got %+v", result)
	}
}

func TestRequiredToolsEmptyConfigPasses(t *testing.T) {
	result, err := (RequiredTools{}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || !strings.Contains(result.Reason, "no required tool names configured") {
		t.Fatalf("expected empty config pass, got %+v", result)
	}
}

func TestRequiredToolsPatternPassesWhenMatchingToolCalled(t *testing.T) {
	result, err := (RequiredTools{Patterns: []string{"route_*"}}).Score(context.Background(), nil, caseWithCalls(
		[]ToolCall{{Name: "search"}, {Name: "route_plan"}},
		nil,
	))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected pattern match pass, got %+v", result)
	}
}

func TestRequiredToolsPatternFailsWhenMissing(t *testing.T) {
	result, err := (RequiredTools{Patterns: []string{"route_*"}}).Score(context.Background(), nil, caseWithCalls(
		[]ToolCall{{Name: "search"}},
		nil,
	))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "route_*") {
		t.Fatalf("expected pattern missing failure, got %+v", result)
	}
}

func TestRequiredToolsUsesTraceToolCalls(t *testing.T) {
	result, err := (RequiredTools{
		Names:    []string{"plan_route"},
		Patterns: []string{"route_*"},
	}).Score(context.Background(), nil, caseWithTraceCalls(
		[]ToolCall{{Name: "plan_route"}, {Name: "route_preview"}},
		nil,
	))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected trace tool calls to satisfy requirements, got %+v", result)
	}
}
