package eval

import (
	"context"
	"testing"
)

type fnJudge func(context.Context, string) (JudgeResponse, error)

func (f fnJudge) Evaluate(ctx context.Context, p string) (JudgeResponse, error) {
	return f(ctx, p)
}

func TestJudge_InterfaceSatisfied(t *testing.T) {
	var j Judge = fnJudge(func(ctx context.Context, p string) (JudgeResponse, error) {
		return JudgeResponse{Score: 0.5, Reason: "test", Tokens: 10}, nil
	})

	resp, err := j.Evaluate(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 0.5 || resp.Reason != "test" || resp.Tokens != 10 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
