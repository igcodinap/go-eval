# Getting Started

This guide starts with a local, deterministic eval you can copy into any Go
project. Then it shows the same shape with an OpenAI-backed judge, RAG metrics,
deterministic checks, `Compound`, `Precheck`, JSONL result output, and
benchmarks.

`go-eval` runs inside `go test`. Eval runs are opt-in: `Runner.Run` and
`eval.Bench` skip unless `GOEVAL=1` is set. This keeps normal local runs and CI
fast by default.

## Install

```bash
go get github.com/igcodinap/go-eval
```

The optional OpenAI judge adapter is a separate module so the core package stays
stdlib-only:

```bash
go get github.com/igcodinap/go-eval/adapters/openai github.com/sashabaranov/go-openai
```

## Copy a Minimal Eval

Create `answer_eval_test.go` next to the code you want to evaluate.

```go
package yourpkg_test

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

func TestAnswerEval(t *testing.T) {
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
}
```

Run it:

```bash
go test ./...
GOEVAL=1 go test ./...
```

Without `GOEVAL=1`, the eval calls skip. With `GOEVAL=1`, `Runner` executes each
metric and reports low scores with `t.Errorf`. Judge or metric execution errors
are fatal.

## Use an OpenAI Judge

Use the adapter when you want actual LLM-as-judge scoring. This is a snippet:
drop it into a test that imports your application code and builds real
`eval.Case` values.

```go
package yourpkg_test

import (
	"os"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	eval "github.com/igcodinap/go-eval"
	openaieval "github.com/igcodinap/go-eval/adapters/openai"
)

func TestOpenAIEval(t *testing.T) {
	if os.Getenv(eval.EnvVar) == "" {
		t.Skip("eval skipped, set GOEVAL=1 to run")
	}

	judge, err := openaieval.NewJudgeFromEnv(openai.GPT4oMini)
	if err != nil {
		t.Fatal(err)
	}

	r := eval.NewRunner(judge, eval.WithResultSink(eval.DefaultResultSink()))
	c := eval.Case{
		Input:   "What is the capital of France?",
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France."},
	}

	r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)
}
```

Run it with both gates:

```bash
OPENAI_API_KEY=sk-... GOEVAL=1 go test ./...
```

The early `GOEVAL` skip keeps ordinary `go test ./...` runs from requiring an
API key.

## Pick Metrics

RAG evals usually start with these metrics:

```go
r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)     // output is supported by context
r.Run(t, eval.Hallucination{Threshold: 0.9}, c)    // output avoids unsupported claims
r.Run(t, eval.AnswerRelevancy{Threshold: 0.7}, c)  // output answers the input
r.Run(t, eval.ContextPrecision{Threshold: 0.7}, c) // retrieved context is relevant
```

Use deterministic metrics for cheap format and content checks before paying for
an LLM judge:

```go
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
```

`Contains`, `Regex`, `JSONPath`, and `FieldCount` ignore the judge argument, but
they still benefit from `Runner` behavior: the same `GOEVAL` gate, case filters,
result sinks, and test assertions.

## Score Multiple Dimensions

`Compound` asks a raw judge for several rubric dimensions in one call. The
OpenAI adapter implements both `eval.Judge` and `eval.RawJudge`, so it can run
`Compound` directly.

```go
r.Run(t, eval.Compound{
	Dimensions: []eval.Dimension{
		{
			Name:      "grounding",
			Rubric:    "Every factual claim is supported by the provided context.",
			Threshold: 0.8,
		},
		{
			Name:      "directness",
			Rubric:    "The answer directly addresses the user question without extra digressions.",
			Threshold: 0.7,
		},
	},
}, c)
```

The result contains an overall score plus per-dimension scores. If any dimension
with a threshold fails, the `Compound` result fails.

## Short-Circuit with Precheck

`Precheck` runs a cheap metric first and only runs the expensive metric when the
precheck passes. This snippet checks that the output looks like JSON before
running a judge-backed custom rubric.

```go
r.Run(t, eval.Precheck{
	Pre: eval.Regex{Pattern: `^\s*\{`},
	Main: eval.GEval{
		Criteria:  "The JSON answer should be grounded in the provided context.",
		Threshold: 0.8,
	},
}, c)
```

If `Pre` fails, `Main` is skipped and the test receives a failed result that
explains the precheck failure.

## Save JSONL Results

Add the default sink when creating the runner:

```go
r := eval.NewRunner(judge, eval.WithResultSink(eval.DefaultResultSink()))
```

Then set `GOEVAL_RESULTS_DIR` during the run:

```bash
GOEVAL=1 GOEVAL_RESULTS_DIR=.eval-results go test ./...
```

`DefaultResultSink` writes `.eval-results/results.jsonl`. Each row includes the
test name, metric, score, pass/fail status, reason, latency, token counts,
optional `Compound` dimensions, and `Case.Metadata`.

You can compare two runs with the optional CLI:

```bash
go install github.com/igcodinap/go-eval/cmd/goeval@latest
goeval compare old/results.jsonl new/results.jsonl
```

## Benchmark an Eval

Use `eval.Bench` when you want benchmark output for latency, token usage, and
score stability.

```go
func BenchmarkAnswerFaithfulness(b *testing.B) {
	r := eval.NewRunner(deterministicJudge{})
	c := eval.Case{
		Input:   "What is the capital of France?",
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France."},
	}

	eval.Bench(b, r, eval.Faithfulness{Threshold: 0.8}, c)
}
```

Run:

```bash
GOEVAL=1 go test -bench=. -count=5
```

Benchmark output includes the normal Go benchmark timing plus:

- `tokens/op`: mean tokens consumed per iteration
- `score_mean`: mean score across iterations
- `score_stddev`: population standard deviation of scores

This makes prompt or model changes easier to compare with `benchstat`.

## Grow into a Suite

For more than a couple of cases, keep cases in a JSON dataset and load them with
`eval.LoadNamedCases`. Use `Case.Metadata` consistently:

| Key | Type | Example |
| --- | --- | --- |
| `flow` | string | `rag.answer` |
| `tier` | string | `critical`, `standard`, or `extended` |
| `dataset` | string | `support-faq/v1` |

The repo also ships a canonical authoring guide for teams and coding agents
building larger suites: [`docs/agent-skills/authoring-go-eval-suites/`](agent-skills/authoring-go-eval-suites/).
It covers case design, metric selection, JSONL reports, and common failure
patterns without requiring any specific agent runtime.
