package eval

import (
	"context"
	"strings"
	"testing"
)

func TestHallucination_Name(t *testing.T) {
	if (Hallucination{}).Name() != "Hallucination" {
		t.Fatalf("Name mismatch")
	}
}

func TestHallucination_RendersPrompt(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.95}}
	c := Case{Output: "Paris is in France.", Context: []string{"Paris is the capital of France."}}

	_, err := Hallucination{}.Score(context.Background(), mj, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !strings.Contains(mj.LastPrompt(), "Paris is in France.") {
		t.Fatalf("prompt missing Output")
	}
}

func TestHallucination_ThresholdDefault0_9(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.89}}
	r, err := Hallucination{}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.9 to fail 0.89")
	}
}

func TestHallucination_PassesAtThreshold(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.95}}
	r, err := Hallucination{Threshold: 0.9}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected Passed=true, got %+v", r)
	}
}
