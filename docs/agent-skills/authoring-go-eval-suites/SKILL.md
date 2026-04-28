---
name: authoring-go-eval-suites
description: Use when writing, running, or reviewing LLM evaluation suites with go-eval; authoring Cases, Metrics, Judges, Runner wiring, interpreting JSONL results, diagnosing score failures, or tuning thresholds.
---

# Authoring go-eval Suites

Tests encode what a Go AI feature should do. Evals measure what its AI outputs actually do. Keep suites gated by `GOEVAL`, deterministic enough for CI, and explicit about what failure means.

This directory is the canonical, agent-agnostic source for the workflow. Agent-specific files such as Claude commands should point here and avoid duplicating the full instructions.

## Use / Avoid

Use this skill to design eval cases, pick `go-eval` metrics, wire a judge, run a suite, read `results.jsonl`, or turn failing scores into recommendations.

Do not use it to implement the agent flow itself, benchmark non-AI code, or change thresholds without evidence from historical runs.

## Decide Mode

- `design`: no eval suite exists, or the user asks for new coverage. Scan agent flows, propose cases, pick metrics, and draft an `_test.go` suite only when asked to write files.
- `run`: an eval suite exists and results are stale or missing. Run with `GOEVAL=1`; prefer `GOEVAL_RESULTS_DIR=.eval-results/`.
- `review`: results exist. Fill the report template and apply recommendation heuristics.

## Quick Model

| Type | Purpose |
|---|---|
| `Case` | Input, output, expected value, context, and metadata for one scenario |
| `Metric` | Stateless scorer: `Score(ctx, judge, case) -> Result` |
| `Judge` | Concurrency-safe LLM provider returning score, reason, and token counts |
| `Runner` | Applies `GOEVAL`, case filters, sinks, and `testing.TB` assertions |
| `Result` | Score, pass/fail, reason, dimensions, latency, tokens, metadata |

## Pick Metrics

| Need | Metric |
|---|---|
| RAG claims supported by retrieved context | `Faithfulness` |
| Output avoids facts outside context | `Hallucination` |
| Output answers the user input | `AnswerRelevancy` |
| Retrieved docs are relevant | `ContextPrecision` |
| Custom rubric | `GEval` |
| Several rubric dimensions in one judge call | `Compound` |
| Cheap format or field checks | `Contains`, `Regex`, `JSONPath`, `FieldCount` |
| Expensive judge only after cheap guard | `Precheck` |

Read `references/metrics-reference.md` when authoring or diagnosing a specific metric.

## Authoring Checklist

1. Define the flow and attach `Case.Metadata["flow"]`, `["tier"]`, and `["dataset"]`.
2. Use table-driven subtests with `t.Parallel()` only if the judge is concurrency-safe.
3. Keep metrics stateless: value types with read-only config during `Score`.
4. Pick the smallest metric set that catches the failure mode.
5. Set thresholds with a short reason; do not copy values blindly.
6. Wire `eval.NewRunner(judge, eval.WithResultSink(eval.DefaultResultSink()))`; `Runner` fills empty `Result.Metric` and zero `Result.Latency`.
7. Start from `assets/templates/` when a concrete suite shape is useful.
8. Keep evals in `_test.go`; preserve `GOEVAL` opt-in behavior for normal suites. If setup creates an external judge before `Runner.Run`, skip early when `os.Getenv(eval.EnvVar) == ""`. If a unit test must force an env gate, use `t.Setenv(...)`.
9. Use `Runner` assertions when possible; custom wrappers should use `tb.Errorf` for low scores and `tb.Fatalf` for judge/internal errors.

## Run

| Task | Command |
|---|---|
| Skip-gated normal tests | `go test ./...` |
| Run evals | `GOEVAL=1 go test ./...` |
| Save JSONL | `GOEVAL=1 GOEVAL_RESULTS_DIR=.eval-results go test ./...` |
| Inspect prompts | `GOEVAL=1 GOEVAL_TRACE=1 go test -v ./...` |
| Bench evals | `GOEVAL=1 go test -bench=. -count=5` |

Use `WithCaseFilter` for tiered runs, usually `tier == "critical"` in fast CI and all tiers in scheduled runs.

## Read Results

Fill `references/report-template.md` after every run or review. Use `references/recommendations.md` to prioritize fixes. Omit unavailable sections cleanly: no sink means no regression, no token split means no prompt/completion budget detail, no `Compound` means no dimensions.

## Common Mistakes

- Stateful metric config shared across parallel tests.
- Judge clients that are not safe under `t.Parallel`.
- `Faithfulness` or `Hallucination` cases missing `Context`.
- Running without `GOEVAL_RESULTS_DIR` when a baseline is needed.
- Lowering thresholds from one bad run instead of fixing repeated failures.
- Comparing scores across different judge models without calling out the model change.
