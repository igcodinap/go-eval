package eval

import (
	"context"
	"strings"
	"testing"
)

func TestFaithfulness_Name(t *testing.T) {
	var m Metric = Faithfulness{}
	if m.Name() != "Faithfulness" {
		t.Fatalf("Name = %q", m.Name())
	}
}

func TestFaithfulness_RendersContextAndOutputIntoPrompt(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9, Reason: "ok"}}

	c := Case{
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France.", "France is in Europe."},
	}
	_, err := Faithfulness{}.Score(context.Background(), mj, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}

	p := mj.LastPrompt()
	if !strings.Contains(p, "Paris is the capital of France.") {
		t.Fatalf("prompt missing Output, got: %q", p)
	}
	if !strings.Contains(p, "France is in Europe.") {
		t.Fatalf("prompt missing Context doc, got: %q", p)
	}
}

func TestFaithfulness_PassesWhenScoreAboveThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9, Reason: "faithful"}}

	r, err := Faithfulness{Threshold: 0.8}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected Passed=true, got %+v", r)
	}
	if r.Score != 0.9 || r.Reason != "faithful" || r.Metric != "Faithfulness" {
		t.Fatalf("unexpected Result: %+v", r)
	}
}

func TestFaithfulness_FailsWhenScoreBelowThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.5, Reason: "bad"}}

	r, err := Faithfulness{Threshold: 0.8}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected Passed=false, got %+v", r)
	}
}

func TestFaithfulness_ZeroThresholdUsesDefault(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.79}}

	r, err := Faithfulness{Threshold: 0}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.8 to fail 0.79")
	}
}
