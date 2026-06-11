# go-eval

> LLM evaluation for Go - `go test` native.

`go-eval` brings LLM-as-judge metrics to the Go ecosystem.
Core metrics (Faithfulness, Hallucination, AnswerRelevancy, ContextPrecision,
ContextRecall, AnswerCorrectness, NoiseSensitivity, GEval, Compound), agent
trajectory checks, structured traces, and repeatability helpers run inside
standard `go test`, with structured artifacts, benchmarks, `-parallel`,
subtests, reports, calibration summaries, and CI integration working out of the
box.

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
      "turns": [
        {"role": "user", "content": "What's the capital of France?"},
        {
          "role": "assistant",
          "tool_calls": [
            {
              "name": "search",
              "arguments": {"query": "capital of France"},
              "result": "Paris is the capital of France."
            }
          ]
        }
      ],
      "expected_tool_calls": [
        {"name": "search", "arguments": {"query": "capital of France"}}
      ],
      "metadata": {
        "flow": "rag.answer",
        "tier": "critical",
        "dataset": "capitals/smoke-v1"
      },
      "artifacts": {
        "state": {"status": "ready"}
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

### Structured traces

Use `Case.Trace` when your agent can emit structured spans, tool calls,
artifacts, or state deltas. `Case.TraceID`, `Result.TraceID`, and JSONL
`trace_id` fields let metric rows, scenario summaries, and trace records join
cleanly in downstream reports.

When both `Case.TraceID` and `Case.Trace.ID` are set, the trace's own ID is
authoritative. `Case.TraceID` seeds an empty `Case.Trace.ID`, and a shared
`Runner` writes each non-empty trace ID to its trace sink at most once.
Tool-call metrics and scenario tool contracts read `Case.Trace` tool-call spans
when present, falling back to `Case.Turns` for legacy evals.

```go
r := eval.NewRunner(
	judge,
	eval.WithResultSink(eval.DefaultResultSink()),
	eval.WithTraceSink(eval.DefaultTraceSink()),
)

c := eval.Case{
	Input:   "Find a route and charge the card",
	Output:  answer,
	TraceID: "route-42",
	Trace: &eval.Trace{
		ID:   "route-42",
		Name: "checkout_route",
		Spans: []eval.Span{{
			Name: "charge",
			Kind: "tool_call",
			ToolCall: &eval.ToolCall{
				Name:      "payments.charge",
				Arguments: json.RawMessage(`{"amount":42}`),
			},
		}},
	},
}
```

When `GOEVAL_RESULTS_DIR` is set, `DefaultTraceSink` writes `traces.jsonl` in
that directory. Trace writes use the same `WithRedactors` hooks as result JSONL.

## Metrics

| Metric             | Measures                                               | Default threshold |
|--------------------|--------------------------------------------------------|-------------------|
| `Faithfulness`     | Output claims supported by Context (RAG)               | 0.8               |
| `Hallucination`    | Output does not invent facts outside Context           | 0.9               |
| `AnswerRelevancy`  | Output addresses Input                                 | 0.7               |
| `ContextPrecision` | Retrieved docs are relevant to Input                   | 0.7               |
| `ContextRecall`    | Retrieved docs contain expected answer/facts           | 0.7               |
| `AnswerCorrectness` | Output matches Expected semantically                  | 0.7               |
| `NoiseSensitivity` | Output ignores distracting retrieved context           | 0.7               |
| `TaskCompletion`   | Agent completed the user task                          | 0.8               |
| `PlanAdherence`    | Agent followed the expected plan                       | 0.7               |
| `ToolArgumentAccuracy` | Tool names and JSON arguments match expectations   | 1.0               |
| `StepEfficiency`   | Trace stays within step and tool-call budgets          | 1.0               |
| `GEval`            | Custom rubric with Criteria and optional Steps         | 0.7               |
| `Compound`         | Multiple rubric dimensions in one judge call           | per-dimension     |
| `Contains`         | Output contains expected substring                      | binary            |
| `Regex`            | Output matches a regex                                 | binary            |
| `JSONPath`         | JSON output value at configured path equals expected   | binary            |
| `FieldCount`       | Minimum non-null top-level JSON field count            | config            |
| `ArtifactExists`   | Named structured artifact exists                       | binary            |
| `ArtifactNotExists` | Named structured artifact does not exist              | binary            |
| `ArtifactJSONPath` | Artifact JSON value at configured path equals expected | binary            |
| `ArtifactFieldCount` | Minimum non-null JSON object field count in artifact | config            |
| `ArtifactNumberLTE` | Artifact JSON number is less than or equal to a max   | binary            |
| `ArtifactArrayContains` | Artifact JSON array contains expected value       | binary            |
| `ArtifactArrayNotContains` | Artifact JSON array excludes expected value  | binary            |
| `ArtifactArrayMinLen` | Artifact JSON array has a minimum length           | binary            |
| `ArtifactSubset`  | Artifact JSON contains a partial expected structure     | binary            |
| `OutputLengthBudget` | Agent output stays within rune/word limits          | config            |
| `Contract`        | Several checks grouped into one named result            | all checks        |
| `ToolCallAccuracy` | Actual tool calls match expected calls by mode         | 1.0               |
| `ToolCallF1`      | Precision/recall/F1 for expected tool calls             | 0.8               |
| `RequiredTools`   | Required tool names were used                          | binary            |
| `ForbiddenTool`   | Disallowed tool names were not used                     | binary            |
| `StepBudget`      | Flattened tool-call count stays within a max            | binary            |
| `Repeat`          | Re-run a metric and aggregate pass rate/score variance  | pass rate 1.0     |

## Artifact Checks

`Case.Artifacts` stores named structured outputs alongside the text `Output`.
Use it for route state, tool traces, intermediate planner state, budget data, or
other JSON payloads that should be checked deterministically before an expensive
judge metric runs.

```go
c := eval.Case{
	Input:  "Plan a prepaid route from Universidad de Santiago to Valparaiso.",
	Output: answer,
	Artifacts: map[string]json.RawMessage{
		"route":  json.RawMessage(`{"status":"ready","total_minutes":98,"stops":["Pajaritos"]}`),
		"budget": json.RawMessage(`{"tokens":742}`),
	},
	Metadata: map[string]any{"case_id": "route-usach-valparaiso-card"},
}

r.Run(t, eval.ArtifactJSONPath{
	Key: "route", Path: "status", Expected: "ready",
}, c)
r.Run(t, eval.ArtifactNumberLTE{
	Key: "route", Path: "total_minutes", Max: 120,
}, c)
r.Run(t, eval.ArtifactArrayContains{
	Key: "route", Path: "stops", Expected: "Pajaritos",
}, c)
r.Run(t, eval.ArtifactArrayNotContains{
	Key: "route", Path: "stops", Expected: "Aeropuerto",
}, c)
r.Run(t, eval.ArtifactArrayMinLen{
	Key: "route", Path: "stops", MinLen: 2,
}, c)
r.Run(t, eval.ArtifactSubset{
	Key:      "route",
	Expected: json.RawMessage(`{"status":"ready"}`),
}, c)
```

Artifact values are `json.RawMessage`: the core stays stdlib-only and does not
interpret artifact names. Conventionally, use stable keys such as `trace`,
`tools`, `route`, `state`, and `budget`.

`ArtifactJSONPath.Expected` uses the same stringified comparison as `JSONPath`:
strings compare as-is, booleans compare as `"true"` or `"false"`, numbers
compare in JSON number form, and objects or arrays compare as compact JSON.
`ArtifactArrayContains`, `ArtifactArrayNotContains`, and `ArtifactSubset`
also support `[*]` wildcard paths such as `stops[*].name`.
For `ArtifactSubset`, expected arrays are order-insensitive subsets: each
expected element must match some actual element.

Use normalizers for deterministic comparisons where casing or Spanish accents
should not matter:

```go
fold := eval.ChainNormalizers(eval.CaseFoldNormalizer(), eval.SpanishASCIIFoldNormalizer())
r.Run(t, eval.ArtifactArrayContains{
	Key: "route", Path: "stops[*].name", Expected: "pájaritos", Normalizer: fold,
}, c)
```

`examples/route_planner/` shows the intended deterministic-first pattern: catch incoherent
workflow state first, then judge final prose only if it is still useful.

Budget wrappers can fail an otherwise passing metric when token or latency
limits are exceeded. They preserve the inner metric score and measurements, then
set `Passed=false` when the budget is overrun:

```go
r.Run(t, eval.WithTokenBudget(1200, eval.Faithfulness{Threshold: 0.8}), c)
r.Run(t, eval.WithLatencyBudget(2*time.Second, eval.AnswerRelevancy{}), c)
r.Run(t, eval.OutputLengthBudget{MaxRunes: 1200, MaxWords: 180}, c)
```

## Trajectory Checks

Use `Case.Turns` and `Case.ExpectedToolCalls` for conversation and tool-use
workflows without leaving the normal `Metric` pipeline:

```go
c := eval.Case{
	Input:    "What's the capital of France?",
	Output:   answer,
	Expected: "Paris",
	Turns: []eval.Turn{
		{Role: eval.RoleUser, Content: "What's the capital of France?"},
		{
			Role: eval.RoleAssistant,
			ToolCalls: []eval.ToolCall{
				{
					Name:      "search",
					Arguments: json.RawMessage(`{"query":"capital of France"}`),
					Result:    "Paris is the capital of France.",
				},
			},
		},
	},
	ExpectedToolCalls: []eval.ToolCall{
		{Name: "search", Arguments: json.RawMessage(`{"query":"capital of France"}`)},
	},
}

r.Run(t, eval.ToolCallAccuracy{Mode: eval.MatchStrict, MatchArgs: true}, c)
r.Run(t, eval.ToolCallF1{MatchArgs: true, Threshold: 0.8}, c)
r.Run(t, eval.RequiredTools{Names: []string{"search"}}, c)
r.Run(t, eval.ForbiddenTool{
	Patterns: []string{"delete_*"},
	Except:   []string{"delete_draft"},
}, c)
r.Run(t, eval.StepBudget{MaxSteps: 2}, c)
```

`ToolCallAccuracy` supports `MatchStrict`, `MatchUnordered`, `MatchSubset`, and
`MatchSuperset`. Tool names match exactly. When `MatchArgs` is set, arguments
are compared as normalized JSON; empty expected arguments are a wildcard. When
`MatchResult` is set, expected non-empty `Result` values must match exactly;
empty expected results are a wildcard. `StepBudget` counts flattened tool calls,
not transcript turns.

`RequiredTools` and `ForbiddenTool` support exact `Names` and glob-style
`Patterns`. `ForbiddenTool.Except` exempts pattern matches only; exact forbidden
names still fail.

`Turn.Name`, `Turn.ToolCallID`, `ToolCall.ID`, and `ToolCall.Error` preserve
provider transcript details for future checks and downstream reports. The
deterministic metrics match against flattened `ToolCall` values rather than raw
turn count or transcript shape.

Use `Repeat` when a judge metric is nondeterministic enough to need a pass-rate
guard:

```go
r.Run(t, eval.Repeat{Metric: eval.Faithfulness{Threshold: 0.8}, N: 3, PassRate: 2.0 / 3.0}, c)
```

## Agent Scenarios

Use `RunScenario` for ordered multi-turn agent flows where each step has its
own tool and artifact contract. The scenario driver is app-owned: `go-eval`
passes accumulated history and artifacts into each step, and the driver returns
only the new turns and artifacts observed for that step.

```go
r := eval.NewRunner(
	judge,
	eval.WithResultSink(eval.DefaultResultSink()),
	eval.WithRedactors(eval.UUIDRedactor(), eval.FieldRedactor("trip_plan_id")),
)

result := r.RunScenario(t, eval.Scenario{
	Name: "planning_to_route_ready",
	Tier: "critical",
	State: map[string]any{"locale": "es-CL"},
	Repeat: eval.ScenarioRepeat{N: 3, PassRate: 2.0 / 3.0},
	Tools: eval.NewToolRegistry("plan_route", "select_map_items"),
	Driver: func(ctx context.Context, req eval.StepRequest) (eval.StepResult, error) {
		return runAgentStep(ctx, req.Step.Input, req.History, req.Artifacts, req.State)
	},
	Steps: []eval.Step{
		{
			Name:                  "greeting",
			Input:                 "Hola",
			ForbiddenToolPatterns: []string{"plan_*", "select_*"},
			MaxToolCalls:          1,
			Timeout:               500 * time.Millisecond,
		},
		{
			Name:                 "ready_route_request",
			Input:                "Propón la ruta",
			RequiredToolPatterns: []string{"plan_*"},
			Timeout:              3 * time.Second,
			Checks: []eval.Metric{
				eval.Contract{ContractName: "ready_route", Checks: []eval.Metric{
					eval.ArtifactJSONPath{Key: "trip_plan", Path: "status", Expected: "ready"},
					eval.ArtifactSubset{Key: "route", Expected: json.RawMessage(`{"success":true}`)},
					eval.ArtifactArrayMinLen{Key: "route", Path: "stops", MinLen: 2},
				}},
			},
		},
	},
})
if !result.Passed {
	t.Fatalf("scenario failed")
}
```

`Scenario.Tools` is optional. When set, observed tool names are checked exactly
against the registry. Required and forbidden checks support exact names plus
glob-style patterns. Contract and metric failures do not stop later steps, so a
failing scenario still writes diagnostic JSONL rows for the remaining steps.
Driver errors and metric execution errors are fatal.

Use `Scenario.State`, `StepRequest.State`, and `StepResult.State` for
driver-to-driver runtime state. This state is copied between steps and stays out
of JSONL unless you also place it in metadata. JSON-like maps and slices are
cloned; custom reference values are treated as opaque and copied by reference, so
keep them immutable or app-owned. Use `Step.Timeout` or
`Case.Timeout` when individual checks need tighter limits than the runner
default.

Set `Step.ExpectFail` for negative cases where a contract or check should fail,
for example an off-topic redirect that must not satisfy a boundary-respect
contract.

Portable scenario definitions can live in JSON while drivers stay app-owned:

```json
{
  "scenarios": [
    {
      "name": "planning_to_route_ready",
      "tier": "critical",
      "driver": "route_agent",
      "tools": ["plan_route", "select_map_items"],
      "repeat": {"n": 2, "pass_rate": 1},
      "steps": [
        {
          "name": "ready_route_request",
          "input": "Propón la ruta",
          "required_tool_patterns": ["plan_*"],
          "required_artifacts": ["route"],
          "max_tool_calls": 3
        }
      ]
    }
  ]
}
```

Bind the named driver in Go before running:

```go
scenarios, err := eval.LoadScenarios("testdata/scenarios.json")
if err != nil {
	t.Fatal(err)
}
scenarios, err = eval.BindScenarioDrivers(scenarios, map[string]eval.StepFunc{
	"route_agent": runRouteAgentStep,
})
if err != nil {
	t.Fatal(err)
}
for _, s := range scenarios {
	r.RunScenario(t, s)
}
```

## vs Python-first eval tools

| Feature                     | Python-first tools   | `go-eval`                    |
|-----------------------------|---------------------|------------------------------|
| Core metrics (RAG)          | yes                 | yes                          |
| Custom LLM-as-judge (GEval) | yes                 | yes                          |
| Runs inside test framework  | pytest              | `go test` / `go test -bench` |
| External platform required  | no                  | no                           |
| Dependencies in core        | pydantic, pytest    | stdlib only                  |
| Structured state artifacts  | yes                 | yes                          |
| Agent / conversation evals  | yes                 | typed turns + traces         |
| Dataset loaders             | YAML/JSON           | JSON in core, YAML deferred  |
| HTML / JSON reports         | yes                 | static HTML/Markdown/JSON    |
| Judge calibration           | yes                 | JSONL disagreement reports   |

`go-eval` is intentionally smaller. It scores RAG-style answers, checks
structured workflow artifacts and tool trajectories, compares and summarizes
JSONL result runs, and uses local judges without adopting a hosted eval
platform.

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

Use `WithRedactors` to scrub result data before JSONL writes:

```go
r := eval.NewRunner(
	judge,
	eval.WithResultSink(eval.DefaultResultSink()),
	eval.WithRedactors(eval.UUIDRedactor(), eval.FieldRedactor("trip_plan_id")),
)
```

Redactors apply to result reasons, Compound dimension reasons, and recursive
string metadata values, including scenario summary metadata and diagnostic
strings. When configured with `WithTraceSink`, redactors also apply to trace
span text, tool-call strings, trace metadata, artifact records, and state
deltas. They do not affect returned `Result` values, raw artifacts, or
`GOEVAL_TRACE` logs.

When `GOEVAL_RESULTS_DIR` is set, `DefaultResultSink` writes
`results.jsonl` in that directory. Each row includes `timestamp`, `test_name`,
`metric`, optional `trace_id`, `score`, `passed`, `reason`, `tokens`, optional
`prompt_tokens` and `completion_tokens`, `latency_ns`, optional `dimensions`, and optional
`metadata`. Scenario runs also write one `_scenario_summary` row with per-step
tool-call names, emitted artifact keys, failed metric names, repeat counts, and
redacted metadata. Repeated scenarios store all emitted trace IDs under
`scenario_summary.trace_ids`; their top-level `trace_id` is left empty because
no single run trace represents the aggregate row. `goeval summarize` excludes
summary rows from metric means.
`Runner` copies `Case.Metadata` into the run result unless a metric sets
`Result.Metadata` explicitly. Metadata cloning follows the same rule as scenario
state: JSON-like map and slice values are cloned, while custom reference values
are copied by reference.

Compare a baseline and current result file with the `compare` package:

```go
report, err := compare.CompareFiles("old/results.jsonl", "new/results.jsonl")
if err != nil {
	// handle malformed JSONL or file errors
}
```

Rows are matched by `test_name` and `metric` by default. Use
`compare.Options.Identity` when a separate case id is stored in metadata. The
`compare.CaseIDFromMetadata` helper adds the configured case id while keeping
`test_name` in the identity for backward-compatible, test-scoped matching. Use
`compare.StableCaseIDFromMetadata`, or a compare policy `case_id_key`, when case
ids should match across test renames. Rows without that metadata key fall back
to `test_name` and `metric`.
Reports include added, missing, improved, regressed, and unchanged entries, with
score, pass/fail, token, latency, and Compound dimension deltas.

For the conventional `Case.Metadata["case_id"]` key, use the stable helper when
case IDs are unique across the suite:

```go
report := compare.CompareWithOptions(
	baseline,
	current,
	compare.Options{Identity: compare.StableCaseIDFromMetadata("")},
)
```

The CLI exposes the same comparison path for CI:

```bash
goeval compare old/results.jsonl new/results.jsonl
goeval summarize current/results.jsonl
goeval report current/results.jsonl --out report.html
goeval report --baseline old/results.jsonl --current new/results.jsonl --format markdown
goeval calibrate --case-id-key case_id --judge-key judge current/results.jsonl
```

`goeval compare` exits nonzero when rows regress or disappear.
`goeval summarize` prints pass/fail and score aggregates for one result file.
`goeval report` renders static HTML, Markdown, or JSON. When `--format` is
omitted, `--out` must use `.html`, `.htm`, `.md`, `.markdown`, or `.json`.
`goeval calibrate` expects repeated rows with judge names in metadata, reports
judge disagreement, aggregates duplicate judge/variant rows by mean score, and
can compare A/B variants with `--pairwise-key variant`.

## Eval profiles and prerequisites

Use `goeval.json` when a repo has different eval run shapes for PRs, nightly
runs, provider-specific checks, or release gates:

```json
{
  "profiles": {
    "pr": {
      "packages": ["./..."],
      "tiers": ["critical"],
      "results_dir": ".goeval/pr"
    },
    "google": {
      "packages": ["./..."],
      "tiers": ["critical", "standard"],
      "results_dir": ".goeval/google",
      "prerequisites": [
        {"type": "env", "name": "GEMINI_API_KEY"},
        {"type": "env", "name": "GOOGLE_ROUTES_API_KEY"}
      ],
      "missing_prerequisite": "skip"
    }
  },
  "compare": {
    "case_id_key": "case_id",
    "default": {
      "score_tolerance": 0.02,
      "fail_on_missing": true,
      "fail_on_regression": true
    }
  }
}
```

Run a profile:

```bash
goeval test --profile pr
goeval test --profile google --config goeval.json -run Route
```

Profiles set `GOEVAL=1`, optionally set `GOEVAL_TIER` and
`GOEVAL_RESULTS_DIR`, preflight manifest prerequisites, and then delegate to
`go test`. Missing prerequisites skip the profile by default; set
`"missing_prerequisite": "fail"` for release-style gates.

Test code can also declare prerequisites directly:

```go
eval.Require(t,
	eval.Env("GEMINI_API_KEY"),
	eval.File("testdata/routes.json"),
	eval.TCP("local routing db", "127.0.0.1:5432"),
)
```

Compare can use a standalone policy file or the `compare` section from
`goeval.json`:

```bash
goeval compare --policy goeval.json --format json old/results.jsonl new/results.jsonl
goeval compare --case-id-key case_id --score-tolerance 0.02 old.jsonl new.jsonl
goeval compare --fail-on-regression=false old.jsonl new.jsonl
goeval summarize --policy goeval.json new/results.jsonl
```

`goeval summarize` now includes pass rate, p95 latency/tokens, scenario run
totals, metadata groupings in the `compare` package, and flaky repeated-case
detection through `compare.SummarizeWithOptions` or
`compare.SummarizeWithPolicy`. Its text output includes metric, tier, flow,
dataset, case, and flaky identity rows.

Use built-in tier filtering for CI and nightly slices:

```go
r := eval.NewRunner(judge, eval.DefaultTierFilter())
```

Then run `GOEVAL=1 GOEVAL_TIER=critical go test ./...`. For custom predicates,
`WithTierFilter` and `WithCaseFilter` are ANDed. `GOEVAL_TIER` is read only
when `DefaultTierFilter()` is installed on the runner.

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

v1.0 is the first stable release of the `go test`-native evaluation core. The
root package remains stdlib-only and keeps the core contracts small: `Judge`,
`Metric`, `Case`, `Result`, `Runner`, structured traces, scenario drivers,
JSONL sinks, deterministic tool/artifact checks, RAG/agent metrics, compare
policies, reliability summaries, reports, and judge calibration.

Future minor releases may add fields to exported structs and new optional
helpers, but v1.x will avoid breaking the public interfaces and CLI workflows
introduced by v1.0.

## Roadmap

The v1.x direction is to keep the core Go-native, local-first, and dependency
free while expanding optional integrations around it.

Likely post-v1.0 work:

1. Optional OpenTelemetry/platform trace bridges outside the root package.
2. YAML or richer dataset loaders in subpackages so the core stays stdlib-only.
3. Additional judge adapters such as Anthropic, Gemini, and Genkit.
4. Release automation for stamped CLI binaries and adapter module tags.
5. More static report views and calibration workflows without requiring a
   hosted dashboard.

## License

MIT
