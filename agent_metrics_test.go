package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTaskCompletionScoreAgent(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{
		Score:            0.9,
		Reason:           "task complete",
		Tokens:           12,
		PromptTokens:     5,
		CompletionTokens: 7,
	}}

	r, err := TaskCompletion{Threshold: 0.8}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}

	if !r.Passed || r.Score != 0.9 || r.Metric != "TaskCompletion" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Tokens != 12 || r.PromptTokens != 5 || r.CompletionTokens != 7 {
		t.Fatalf("unexpected token fields: %+v", r)
	}
	assertPromptContains(t, mj.LastPrompt(), "Where is my order?", "orders.lookup", "arrives tomorrow")
}

func TestTaskCompletionDefaultThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.79}}

	r, err := TaskCompletion{}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.8 to fail 0.79")
	}
}

func TestToolUseCorrectnessScoreAgent(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.85, Reason: "tool use was correct"}}

	r, err := ToolUseCorrectness{Threshold: 0.8}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}

	if !r.Passed || r.Metric != "ToolUseCorrectness" {
		t.Fatalf("unexpected result: %+v", r)
	}
	assertPromptContains(t, mj.LastPrompt(), "kind=tool", "orders.lookup", "delivery status")
}

func TestToolUseCorrectnessDefaultThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.79}}

	r, err := ToolUseCorrectness{}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.8 to fail 0.79")
	}
}

func TestAgentGEvalIncludesCriteriaStepsAndTrace(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9}}

	_, err := AgentGEval{
		Criteria: "The agent must answer with a concise status update.",
		Steps:    []string{"Check task completion.", "Check trace consistency."},
	}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}

	assertPromptContains(t, mj.LastPrompt(),
		"The agent must answer with a concise status update.",
		"1. Check task completion.",
		"2. Check trace consistency.",
		"orders.lookup",
	)
}

func TestAgentGEvalDefaultThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.69}}

	r, err := AgentGEval{Criteria: "x"}.ScoreAgent(context.Background(), mj, AgentCase{})
	if err != nil {
		t.Fatalf("ScoreAgent: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.7 to fail 0.69")
	}
}

func TestAgentMetricReturnsJudgeError(t *testing.T) {
	sentinel := errors.New("malformed judge response")
	mj := &MockJudge{Err: sentinel}

	_, err := TaskCompletion{}.ScoreAgent(context.Background(), mj, sampleAgentCase())
	if !errors.Is(err, sentinel) {
		t.Fatalf("ScoreAgent err=%v, want %v", err, sentinel)
	}
}

func sampleAgentCase() AgentCase {
	return AgentCase{
		Messages: []Message{
			{Role: RoleUser, Content: "Where is my order?"},
			{Role: RoleAssistant, Content: "I will check the order system."},
		},
		Output:   "Your order arrives tomorrow.",
		Expected: "Answer with delivery status.",
		Context:  []string{"Order 42 delivery date: tomorrow."},
		Trace: []TraceSpan{
			{
				Kind:   SpanTool,
				Name:   "orders.lookup",
				Input:  "order_id=42",
				Output: "delivery_date=tomorrow",
			},
		},
	}
}

func assertPromptContains(t *testing.T, prompt string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
