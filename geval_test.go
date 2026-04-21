package eval

import (
	"context"
	"strings"
	"testing"
)

func TestGEval_Name(t *testing.T) {
	if (GEval{}).Name() != "GEval" {
		t.Fatalf("Name mismatch")
	}
}

func TestGEval_IncludesCriteriaInPrompt(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9}}
	_, err := GEval{
		Criteria: "Output should be polite and concise.",
	}.Score(context.Background(), mj, Case{Input: "hi", Output: "hello"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !strings.Contains(mj.LastPrompt(), "Output should be polite and concise.") {
		t.Fatalf("prompt missing Criteria")
	}
}

func TestGEval_IncludesStepsWhenProvided(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9}}
	_, err := GEval{
		Criteria: "x",
		Steps:    []string{"Check tone.", "Check length."},
	}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	p := mj.LastPrompt()
	if !strings.Contains(p, "1. Check tone.") || !strings.Contains(p, "2. Check length.") {
		t.Fatalf("prompt missing numbered Steps, got: %q", p)
	}
}

func TestGEval_OmitsStepsSectionWhenEmpty(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.9}}
	_, err := GEval{Criteria: "x"}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if strings.Contains(mj.LastPrompt(), "EVALUATION STEPS") {
		t.Fatalf("prompt should omit EVALUATION STEPS when Steps empty")
	}
}

func TestGEval_DefaultThresholdIs0_7(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.69}}
	r, err := GEval{Criteria: "x"}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.7 to fail 0.69")
	}
}
