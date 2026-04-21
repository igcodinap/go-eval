package eval

import (
	"context"
	"strings"
	"testing"
)

func TestAnswerRelevancy_Name(t *testing.T) {
	if (AnswerRelevancy{}).Name() != "AnswerRelevancy" {
		t.Fatalf("Name mismatch")
	}
}

func TestAnswerRelevancy_DefaultNumQuestionsIs3(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.8}}
	_, err := AnswerRelevancy{}.Score(context.Background(), mj, Case{Input: "i", Output: "o"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !strings.Contains(mj.LastPrompt(), "3 alternative questions") {
		t.Fatalf("prompt missing default NumQuestions=3, got: %q", mj.LastPrompt())
	}
}

func TestAnswerRelevancy_CustomNumQuestions(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.8}}
	_, err := AnswerRelevancy{NumQuestions: 5}.Score(context.Background(), mj, Case{Input: "i", Output: "o"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !strings.Contains(mj.LastPrompt(), "5 alternative questions") {
		t.Fatalf("prompt missing NumQuestions=5, got: %q", mj.LastPrompt())
	}
}

func TestAnswerRelevancy_DefaultThresholdIs0_7(t *testing.T) {
	mj := &MockJudge{Response: JudgeResponse{Score: 0.69}}
	r, err := AnswerRelevancy{}.Score(context.Background(), mj, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected default threshold 0.7 to fail 0.69")
	}
}
