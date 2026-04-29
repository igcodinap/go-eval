# go-eval

> LLM evaluation for Go - `go test` native.

`go-eval` brings LLM-as-judge metrics to the Go ecosystem.
Core metrics (Faithfulness, Hallucination, AnswerRelevancy, ContextPrecision,
GEval, Compound) and deterministic checks run inside standard `go test`, with
benchmarks, `-parallel`, subtests, and CI integration working out of the box.

## Why

Python has mature LLM evaluation tooling. Go had Levenshtein distance and
blog-post hacks. `go-eval` fills the gap with a stdlib-only core, native
`testing.T` integration, and zero external platform requirements.

## Install

```bash
go get github.com/igcodinap/go-eval
```

Install the optional CLI:

```bash
go install github.com/igcodinap/go-eval/cmd/goeval@latest
```

Optional judge adapters live in separate modules so the core package stays
stdlib-only:

```bash
go get github.com/igcodinap/go-eval/adapters/ollama
go get github.com/igcodinap/go-eval/adapters/openai github.com/sashabaranov/go-openai
```

## Quickstart

For a full walkthrough with copyable evals, an OpenAI-backed judge, JSONL
results, and benchmarks, see the [Getting Started guide](docs/getting-started.md).

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

Or use the thin CLI wrapper:

```bash
goeval test ./...
```

Unset `GOEVAL` and evals skip. That keeps CI and local runs safe by default.

## Datasets

Keep golden cases in JSON when you want eval data outside Go test code:

```json
{
  "cases": [
    {
      "name": "france-capital",
      "input": "What's the capital of France?",
      "expected": "Paris",
      "context": ["Paris is the capital of France."],
      "metadata": {
        "flow": "rag.answer",
        "tier": "critical",
        "dataset": "capitals/smoke-v1"
      }
    }
  ]
}
```

Use `LoadNamedCases` for table-driven tests:

```go
cases, err := eval.LoadNamedCases("testdata/cases.json")
if err != nil {
	t.Fatal(err)
}

for _, tc := range cases {
	tc := tc
	t.Run(tc.Name, func(t *testing.T) {
		t.Parallel()

		c := tc.Case
		c.Output, c.Context = runRAG(c.Input)

		r.Run(t, eval.Faithfulness{Threshold: 0.8}, c)
	})
}
```

Use `LoadCases` when names are not needed. The loader is JSON-only and
stdlib-only; YAML support is deferred to a future subpackage or module so the
core package stays dependency-free.

### Tracing judge I/O

Set `GOEVAL_TRACE=1` alongside `GOEVAL=1` to dump every judge prompt and
response via `t.Log`. Output respects `-v` and test buffering.

```bash
GOEVAL=1 GOEVAL_TRACE=1 go test -v -run TestFaithfulness
```

> **Warning:** traces contain full prompt + response text. May include PII
> or sensitive eval payloads. Do not enable in shared CI logs.

## Metrics

| Metric             | Measures                                               | Default threshold |
|--------------------|--------------------------------------------------------|-------------------|
| `Faithfulness`     | Output claims supported by Context (RAG)               | 0.8               |
| `Hallucination`    | Output does not invent facts outside Context           | 0.9               |
| `AnswerRelevancy`  | Output addresses Input                                 | 0.7               |
| `ContextPrecision` | Retrieved docs are relevant to Input                   | 0.7               |
| `GEval`            | Custom rubric with Criteria and optional Steps         | 0.7               |
| `Compound`         | Multiple rubric dimensions in one judge call           | per-dimension     |
| `Contains`         | Output contains expected substring                      | binary            |
| `Regex`            | Output matches a regex                                 | binary            |
| `JSONPath`         | JSON output value at configured path equals expected   | binary            |
| `FieldCount`       | Minimum non-null top-level JSON field count            | config            |

## vs Python-first eval tools

| Feature                     | Python-first tools   | `go-eval`                    |
|-----------------------------|---------------------|------------------------------|
| Core metrics (RAG)          | yes                 | yes                          |
| Custom LLM-as-judge (GEval) | yes                 | yes                          |
| Runs inside test framework  | pytest              | `go test` / `go test -bench` |
| External platform required  | no                  | no                           |
| Dependencies in core        | pydantic, pytest    | stdlib only                  |
| Agent / conversation evals  | yes                 | planned                      |
| Dataset loaders             | YAML/JSON           | JSON in core, YAML deferred  |
| HTML / JSON reports         | yes                 | via `go test -json`          |

`go-eval` is intentionally smaller. v0.3 covers the common case:
scoring RAG-style and deterministic evaluation cases in a CI-friendly way,
loading JSON datasets, comparing JSONL result runs, and using local Ollama
judges.

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

## Result JSONL

Configure a sink to persist one JSON object per metric run:

```go
r := eval.NewRunner(judge, eval.WithResultSink(eval.DefaultResultSink()))
```

When `GOEVAL_RESULTS_DIR` is set, `DefaultResultSink` writes
`results.jsonl` in that directory. Each row includes `timestamp`, `test_name`,
`metric`, `score`, `passed`, `reason`, `tokens`, optional `prompt_tokens` and
`completion_tokens`, `latency_ns`, optional `dimensions`, and optional
`metadata`. `Runner` copies `Case.Metadata` into the run result unless a metric
sets `Result.Metadata` explicitly.

Compare a baseline and current result file with the `compare` package:

```go
report, err := compare.CompareFiles("old/results.jsonl", "new/results.jsonl")
if err != nil {
	// handle malformed JSONL or file errors
}
```

Rows are matched by `test_name` and `metric` by default. Use
`compare.Options.Identity` when a separate case id is stored in metadata.
Reports include added, missing, improved, regressed, and unchanged entries, with
score, pass/fail, token, latency, and Compound dimension deltas.

The CLI exposes the same comparison path for CI:

```bash
goeval compare old/results.jsonl new/results.jsonl
```

`goeval compare` exits nonzero when rows regress or disappear.

Use `WithCaseFilter` to run a selected slice of cases, for example a
critical-only CI path:

```go
r := eval.NewRunner(judge, eval.WithCaseFilter(func(c eval.Case) bool {
	return c.Metadata["tier"] == "critical"
}))
```

## Ollama Judge Adapter

Use the Ollama adapter when you want local LLM-as-judge scoring:

```go
package yourpkg_test

import (
	"os"
	"testing"

	eval "github.com/igcodinap/go-eval"
	ollamaeval "github.com/igcodinap/go-eval/adapters/ollama"
)

func TestOllamaEval(t *testing.T) {
	if os.Getenv(eval.EnvVar) == "" {
		t.Skip("eval skipped, set GOEVAL=1 to run")
	}

	judge := ollamaeval.NewJudge("llama3.2")
	r := eval.NewRunner(judge)

	r.Run(t, eval.Faithfulness{Threshold: 0.8}, eval.Case{
		Input:   "What is the capital of France?",
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France."},
	})
}
```

For non-default servers, configure the local endpoint and HTTP client:

```go
judge := ollamaeval.NewJudge(
	"llama3.2",
	ollamaeval.WithBaseURL("http://localhost:11434"),
	ollamaeval.WithHTTPClient(http.DefaultClient),
)
```

## Agent skill

The repo ships an agentskills.io-style guide for coding agents that need to
author, run, or review `go-eval` suites. The canonical, agent-agnostic source
lives at [`docs/agent-skills/authoring-go-eval-suites/`](docs/agent-skills/authoring-go-eval-suites/).

Claude Code users can invoke the companion `/eval` command from
`.claude/commands/eval.md`. The command is a thin adapter that infers design,
run, or review mode from repo state and uses the skill's report template and
recommendation heuristics.

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

v0.3 - JSON datasets, result comparison, Ollama and OpenAI adapter modules,
Compound, deterministic metrics, and opt-in result sinks are included. API may
change before v1.0.

## Roadmap

Planned scope:
1. Conversation evaluation model (`ConversationCase`, `ConversationMetric`, `RunConversation`)
2. YAML loader submodule (core remains stdlib-only)
3. Additional adapters beyond Ollama (`Genkit`, `Anthropic`, `Gemini`)

## License

MIT
