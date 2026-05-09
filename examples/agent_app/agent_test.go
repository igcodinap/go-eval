package main

import (
	"context"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

type scriptedJudge struct{}

func (scriptedJudge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	_ = ctx
	_ = prompt
	return eval.JudgeResponse{Score: 0.9, Reason: "canned demo response", Tokens: 42}, nil
}

func TestAgentEvalSuite(t *testing.T) {
	t.Setenv(eval.EnvVar, "1")

	agent := &Agent{Orders: map[string]string{"42": "arrives tomorrow"}}
	r := eval.NewRunner(scriptedJudge{})

	cases := []struct {
		name string
		eval.AgentCase
	}{
		{
			name: "order-status",
			AgentCase: eval.AgentCase{
				Messages: []eval.Message{
					{Role: eval.RoleUser, Content: "Where is my order?"},
				},
				Expected: "Answer with the delivery status.",
				Metadata: map[string]any{
					"flow":    "support.order_status",
					"tier":    "critical",
					"dataset": "agent-demo/v1",
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := tc.AgentCase
			c.Output, c.Trace = agent.Answer(c.Messages[0].Content)

			r.RunAgent(t, eval.TaskCompletion{Threshold: 0.8}, c)
			r.RunAgent(t, eval.ToolUseCorrectness{Threshold: 0.8}, c)
		})
	}
}
