package main

import (
	"context"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

type scriptedJudge struct{}

func (scriptedJudge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	return eval.JudgeResponse{Score: 0.9, Reason: "canned demo response", Tokens: 50}, nil
}

func TestRAGEvalSuite(t *testing.T) {
	p := &Pipeline{Docs: []string{
		"Paris is the capital of France.",
		"Rome is the capital of Italy.",
		"Madrid is the capital of Spain.",
	}}

	r := eval.NewRunner(scriptedJudge{})

	cases, err := eval.LoadNamedCases("testdata/cases.json")
	if err != nil {
		t.Fatalf("LoadNamedCases: %v", err)
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			c := tc.Case
			answer, docs := p.Answer(c.Input)
			c.Output = answer
			c.Context = docs

			r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)
			r.Run(t, eval.Hallucination{Threshold: 0.9}, c)
			r.Run(t, eval.AnswerRelevancy{Threshold: 0.7}, c)
			r.Run(t, eval.ContextPrecision{Threshold: 0.7}, c)
			r.Run(t, eval.GEval{
				Criteria:  "Output should directly answer the question.",
				Threshold: 0.7,
			}, c)
		})
	}
}
