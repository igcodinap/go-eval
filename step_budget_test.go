package eval

import (
	"context"
	"strings"
	"testing"
)

func TestStepBudget(t *testing.T) {
	tests := []struct {
		name     string
		maxSteps int
		calls    []ToolCall
		score    float64
		passed   bool
		reason   string
	}{
		{name: "disabled", maxSteps: 0, calls: []ToolCall{{Name: "search"}}, score: 1, passed: true, reason: "disabled"},
		{name: "empty", maxSteps: 2, score: 1, passed: true, reason: "0 <= 2"},
		{name: "under", maxSteps: 3, calls: []ToolCall{{Name: "search"}}, score: 1, passed: true, reason: "1 <= 3"},
		{name: "exact", maxSteps: 2, calls: []ToolCall{{Name: "search"}, {Name: "lookup"}}, score: 1, passed: true, reason: "2 <= 2"},
		{name: "over", maxSteps: 1, calls: []ToolCall{{Name: "search"}, {Name: "lookup"}}, score: 0.5, passed: false, reason: "2 > 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (StepBudget{MaxSteps: tt.maxSteps}).Score(context.Background(), nil, caseWithCalls(tt.calls, nil))
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if result.Score != tt.score || result.Passed != tt.passed || !strings.Contains(result.Reason, tt.reason) {
				t.Fatalf("result = %+v, want score %v passed %v reason containing %q", result, tt.score, tt.passed, tt.reason)
			}
		})
	}
}

func TestStepBudgetCountsToolCallsAcrossTurns(t *testing.T) {
	c := Case{
		Turns: []Turn{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "search"}}},
			{Role: RoleTool, Content: "result"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "lookup"}}},
		},
	}

	result, err := (StepBudget{MaxSteps: 1}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0.5 || !strings.Contains(result.Reason, "2 > 1") {
		t.Fatalf("expected two flattened tool calls to exceed budget, got %+v", result)
	}
}

func TestStepBudgetUsesTraceToolCallsWhenPresent(t *testing.T) {
	c := caseWithTraceCalls([]ToolCall{{Name: "search"}, {Name: "lookup"}}, nil)
	c.Turns = []Turn{{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "turn_only"}}}}

	result, err := (StepBudget{MaxSteps: 1}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0.5 || !strings.Contains(result.Reason, "2 > 1") {
		t.Fatalf("expected trace tool calls to exceed budget, got %+v", result)
	}
}

func TestStepBudgetWorksAsPrecheck(t *testing.T) {
	main := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 1, Passed: true, Metric: "Faithfulness"},
	}
	metric := Precheck{
		Pre:  StepBudget{MaxSteps: 1},
		Main: main,
	}

	result, err := metric.Score(context.Background(), nil, caseWithCalls([]ToolCall{{Name: "a"}, {Name: "b"}}, nil))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || main.calls != 0 {
		t.Fatalf("expected precheck failure without main call, result=%+v calls=%d", result, main.calls)
	}
}
