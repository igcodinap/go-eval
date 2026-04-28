package docs_test

import (
	"context"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

type deterministicJudge struct{}

func (deterministicJudge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	_ = ctx
	_ = prompt
	return eval.JudgeResponse{
		Score:  0.95,
		Reason: "local deterministic judge response",
		Tokens: 12,
	}, nil
}

func (deterministicJudge) EvaluateRaw(ctx context.Context, prompt string) (eval.RawJudgeResponse, error) {
	_ = ctx
	_ = prompt
	return eval.RawJudgeResponse{
		Content: `{
			"grounding": {"score": 0.95, "reason": "claims are supported by context"},
			"directness": {"score": 0.90, "reason": "answer is direct"}
		}`,
		Tokens: 18,
	}, nil
}

func TestGettingStartedPatterns(t *testing.T) {
	t.Setenv(eval.EnvVar, "1")

	judge := deterministicJudge{}
	r := eval.NewRunner(judge, eval.WithResultSink(eval.DefaultResultSink()))

	c := eval.Case{
		Input:    "What is the capital of France?",
		Output:   "Paris is the capital of France.",
		Expected: "Paris",
		Context:  []string{"Paris is the capital of France."},
		Metadata: map[string]any{
			"flow":    "rag.answer",
			"tier":    "critical",
			"dataset": "getting-started/v1",
		},
	}

	r.Run(t, eval.Contains{}, c)
	r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)
	r.Run(t, eval.Hallucination{Threshold: 0.9}, c)
	r.Run(t, eval.AnswerRelevancy{Threshold: 0.7}, c)
	r.Run(t, eval.Compound{
		Dimensions: []eval.Dimension{
			{
				Name:      "grounding",
				Rubric:    "Every factual claim is supported by the provided context.",
				Threshold: 0.8,
			},
			{
				Name:      "directness",
				Rubric:    "The answer directly addresses the user question.",
				Threshold: 0.7,
			},
		},
	}, c)
}

func TestGettingStartedDeterministicMetrics(t *testing.T) {
	t.Setenv(eval.EnvVar, "1")

	r := eval.NewRunner(deterministicJudge{})

	r.Run(t, eval.Contains{}, eval.Case{
		Output:   "Paris is the capital of France.",
		Expected: "Paris",
	})
	r.Run(t, eval.Regex{Pattern: `(?i)\bparis\b`}, eval.Case{
		Output: "Paris is the capital of France.",
	})
	r.Run(t, eval.MustJSONPath("answer.city"), eval.Case{
		Output:   `{"answer":{"city":"Paris","country":"France"}}`,
		Expected: "Paris",
	})
	r.Run(t, eval.FieldCount{MinFields: 2}, eval.Case{
		Output: `{"answer":"Paris","confidence":0.98}`,
	})
	r.Run(t, eval.Precheck{
		Pre: eval.Regex{Pattern: `^\s*\{`},
		Main: eval.GEval{
			Criteria:  "The JSON answer should be grounded in the provided context.",
			Threshold: 0.8,
		},
	}, eval.Case{
		Output:  `{"answer":"Paris"}`,
		Context: []string{"Paris is the capital of France."},
	})
}

func BenchmarkGettingStartedFaithfulness(b *testing.B) {
	r := eval.NewRunner(deterministicJudge{})
	c := eval.Case{
		Input:   "What is the capital of France?",
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France."},
	}

	eval.Bench(b, r, eval.Faithfulness{Threshold: 0.8}, c)
}
