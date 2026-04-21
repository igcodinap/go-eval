# go-eval

> LLM evaluation for Go - `go test` native.

`go-eval` brings `deepeval`-style LLM-as-judge metrics to the Go ecosystem.
Five core metrics (Faithfulness, Hallucination, AnswerRelevancy,
ContextPrecision, GEval) run inside standard `go test`, with benchmarks,
`-parallel`, subtests, and CI integration working out of the box.

## Why

Python has `deepeval`, `ragas`, and `braintrust`. Go had Levenshtein
distance and blog-post hacks. `go-eval` fills the gap with a stdlib-only
core, native `testing.T` integration, and zero external platform
requirements.

## Install

```bash
go get github.com/igcodinap/go-eval
```

## Quickstart

```go
package yourpkg_test

import (
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestRAGAnswer(t *testing.T) {
	judge := newMyJudge(t) // your Judge impl (see examples/openai_judge)
	r := eval.NewRunner(judge)

	c := eval.Case{
		Input:   "What's the capital of France?",
		Output:  myRAG.Answer("What's the capital of France?"),
		Context: []string{"Paris is the capital of France..."},
	}

	r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)
	r.Run(t, eval.Hallucination{Threshold: 0.9}, c)
	r.Run(t, eval.AnswerRelevancy{Threshold: 0.7}, c)
}
```

Run:

```bash
GOEVAL=1 go test ./...
```

Unset `GOEVAL` and evals skip. That keeps CI and local runs safe by default.

## Metrics

| Metric             | Measures                                               | Default threshold |
|--------------------|--------------------------------------------------------|-------------------|
| `Faithfulness`     | Output claims supported by Context (RAG)               | 0.8               |
| `Hallucination`    | Output does not invent facts outside Context           | 0.9               |
| `AnswerRelevancy`  | Output addresses Input                                 | 0.7               |
| `ContextPrecision` | Retrieved docs are relevant to Input                   | 0.7               |
| `GEval`            | Custom rubric with Criteria and optional Steps         | 0.7               |

## vs `deepeval`

| Feature                     | `deepeval` (Python) | `go-eval`                    |
|-----------------------------|---------------------|------------------------------|
| Core metrics (RAG)          | yes                 | yes                          |
| Custom LLM-as-judge (GEval) | yes                 | yes                          |
| Runs inside test framework  | pytest              | `go test` / `go test -bench` |
| External platform required  | no                  | no                           |
| Dependencies in core        | pydantic, pytest    | stdlib only                  |
| Agent / conversation evals  | yes                 | no (post-v1)                 |
| Dataset loaders (YAML/JSON) | yes                 | no (post-v1)                 |
| HTML / JSON reports         | yes                 | via `go test -json`          |

`go-eval` is intentionally smaller. v0.1 covers the common case:
scoring individual RAG-style cases with LLM-as-judge metrics in a CI-friendly
way.

## Benchmarks

```go
func BenchmarkRAGLatency(b *testing.B) {
	r := eval.NewRunner(newMyJudge(b))
	c := eval.Case{Input: "...", Output: "...", Context: docs}

	eval.Bench(b, r, eval.Faithfulness{Threshold: 0.8}, c)
}
```

```bash
GOEVAL=1 go test -bench=. -count=5 > old.txt
# change a prompt or model
GOEVAL=1 go test -bench=. -count=5 > new.txt
benchstat old.txt new.txt
```

`eval.Bench` reports `ns/op`, `tokens/op`, `score_mean`, and `score_stddev`.

## Writing your own `Judge`

```go
type MyJudge struct{}

func (j *MyJudge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	// 1. Send prompt to an LLM.
	// 2. Parse its JSON {"score": float, "reason": string} response.
	// 3. Return eval.JudgeResponse{Score, Reason, Tokens}.
	// Must be safe for concurrent use.
	return eval.JudgeResponse{}, nil
}
```

See `examples/openai_judge/` for a reference implementation.

## Status

v0.1 - core metrics, Runner, and benchmarks. API may change before v1.0.

## License

MIT
