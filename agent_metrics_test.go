package eval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTaskCompletionIncludesTraceAndPassesThreshold(t *testing.T) {
	judge := &MockJudge{Response: JudgeResponse{Score: 0.91, Reason: "complete"}}
	result, err := (TaskCompletion{Threshold: 0.9}).Score(context.Background(), judge, Case{
		Input:    "Book the route",
		Output:   "Route booked",
		Expected: "A route is booked",
		Trace: &Trace{
			ID: "trace-1",
			Spans: []Span{{
				Name:   "plan",
				Output: "planned",
			}},
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Metric != "TaskCompletion" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if prompt := judge.LastPrompt(); !strings.Contains(prompt, "Book the route") || !strings.Contains(prompt, "trace-1") {
		t.Fatalf("prompt missing case or trace:\n%s", prompt)
	}
}

func TestTaskCompletionReturnsJudgeError(t *testing.T) {
	judge := &MockJudge{Err: errors.New("offline")}
	_, err := (TaskCompletion{}).Score(context.Background(), judge, Case{Input: "i", Output: "o"})
	if err == nil || !strings.Contains(err.Error(), "task_completion: judge") {
		t.Fatalf("expected judge error, got %v", err)
	}
}

func TestToolArgumentAccuracyUsesTraceCalls(t *testing.T) {
	result, err := (ToolArgumentAccuracy{}).Score(context.Background(), nil, Case{
		ExpectedToolCalls: []ToolCall{{
			Name:      "search",
			Arguments: json.RawMessage(`{"q":"go"}`),
		}},
		Trace: &Trace{
			Spans: []Span{{
				Kind: "tool_call",
				ToolCall: &ToolCall{
					Name:      "search",
					Arguments: json.RawMessage(`{"q":"go"}`),
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestToolCallsFromCaseFallsBackToTurnsWhenTraceHasNoToolCalls(t *testing.T) {
	calls := toolCallsFromCase(Case{
		Trace: &Trace{Spans: []Span{{
			Name: "planning",
			Kind: "scenario_step",
		}}},
		Turns: []Turn{{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{Name: "turn_tool"}},
		}},
	})
	if len(calls) != 1 || calls[0].Name != "turn_tool" {
		t.Fatalf("expected turns fallback when trace has no tool calls, got %+v", calls)
	}
}

func TestToolArgumentAccuracyFailsMalformedArguments(t *testing.T) {
	result, err := (ToolArgumentAccuracy{}).Score(context.Background(), nil, Case{
		ExpectedToolCalls: []ToolCall{{
			Name:      "search",
			Arguments: json.RawMessage(`{"q":"go"}`),
		}},
		Turns: []Turn{{
			ToolCalls: []ToolCall{{
				Name:      "search",
				Arguments: json.RawMessage(`{bad`),
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "actual arguments are not valid JSON") {
		t.Fatalf("expected malformed argument failure, got %+v", result)
	}
}

func TestPlanAdherenceUsesMetadataPlanAndJudge(t *testing.T) {
	judge := &MockJudge{Response: JudgeResponse{Score: 0.8, Reason: "followed"}}
	result, err := (PlanAdherence{Threshold: 0.8}).Score(context.Background(), judge, Case{
		Output: "done",
		Metadata: map[string]any{
			"plan": []string{"search", "answer"},
		},
		Turns: []Turn{{Role: RoleAssistant, Content: "searched"}},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || !strings.Contains(judge.LastPrompt(), "search") {
		t.Fatalf("unexpected result/prompt: %+v\n%s", result, judge.LastPrompt())
	}
}

func TestPlanAdherenceFailsWithoutPlan(t *testing.T) {
	result, err := (PlanAdherence{}).Score(context.Background(), &MockJudge{}, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Reason != "expected plan is empty" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestStepEfficiencyChecksTraceBudgets(t *testing.T) {
	result, err := (StepEfficiency{MaxSteps: 1, MaxToolCalls: 1}).Score(context.Background(), nil, Case{
		Trace: &Trace{
			Spans: []Span{
				{Name: "step-1", Kind: "agent"},
				{Name: "step-2", Kind: "agent"},
				{Kind: "tool_call", ToolCall: &ToolCall{Name: "search"}},
				{Kind: "tool_call", ToolCall: &ToolCall{Name: "lookup"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0.5 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
