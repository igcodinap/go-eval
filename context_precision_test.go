package eval

import (
	"context"
	"strings"
	"testing"
)

func TestContextPrecision_Name(t *testing.T) {
	if (ContextPrecision{}).Name() != "ContextPrecision" {
		t.Fatalf("Name mismatch")
	}
}

func TestContextPrecision_IncludesInputAndDocs(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.8}}
	c := Case{
		Input:   "What's the capital of France?",
		Context: []string{"Paris is the capital.", "France is in Europe."},
	}
	_, err := ContextPrecision{}.Score(context.Background(), mj, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	p := mj.LastPrompt()
	if !strings.Contains(p, "What's the capital of France?") {
		t.Fatalf("prompt missing Input")
	}
	if !strings.Contains(p, "Paris is the capital.") {
		t.Fatalf("prompt missing first doc")
	}
}

func TestContextPrecision_DefaultThresholdIs0_7(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.65}}
	r, err := ContextPrecision{}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.7 to fail 0.65")
	}
}
