package eval

import (
	"context"
	"errors"
	"testing"
)

func TestMockJudge_ReturnsConfiguredResponse(t *testing.T) {
	mj := &MockJudge{
		Response: JudgeResponse{Score: 0.42, Reason: "canned", Tokens: 7},
	}

	resp, err := mj.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 0.42 || resp.Reason != "canned" || resp.Tokens != 7 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if mj.Calls() != 1 {
		t.Fatalf("Calls: got %d, want 1", mj.Calls())
	}
	if got := mj.LastPrompt(); got != "prompt" {
		t.Fatalf("LastPrompt: got %q, want %q", got, "prompt")
	}
}

func TestMockJudge_ReturnsConfiguredError(t *testing.T) {
	sentinel := errors.New("judge unavailable")
	mj := &MockJudge{Err: sentinel}

	_, err := mj.Evaluate(context.Background(), "p")
	if !errors.Is(err, sentinel) {
		t.Fatalf("got err=%v, want %v", err, sentinel)
	}
}

func TestMockJudge_CustomFunc(t *testing.T) {
	mj := &MockJudge{
		Func: func(ctx context.Context, prompt string) (JudgeResponse, error) {
			return JudgeResponse{Score: float64(len(prompt)) / 10}, nil
		},
	}

	resp, err := mj.Evaluate(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 0.5 {
		t.Fatalf("Score: got %v, want 0.5", resp.Score)
	}
}
