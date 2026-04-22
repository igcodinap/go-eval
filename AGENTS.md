# go-eval — Agent Instructions

## What this is

LLM evaluation library for Go. Brings deepeval-style LLM-as-judge metrics into `go test`. Stdlib-only core, zero external platform requirements.

## Commands

```
go test -race ./...          # all tests
golangci-lint run            # lint
go test -bench=. -count=5    # benchmarks
```

**Evals require `GOEVAL=1`**. Unset and all `Runner.Run` calls skip. This keeps CI fast by default.

```
GOEVAL=1 go test ./...
GOEVAL=1 go test -bench=. -count=5 > old.txt
```

## Architecture

Core interfaces in root package:

| Type | Purpose |
|------|---------|
| `Judge` | LLM provider abstraction — sends prompt, returns `{score, reason, tokens}`. Must be concurrency-safe. |
| `Metric` | Evaluation contract — `Score(ctx, judge, case) → Result`. Stateless value types. |
| `Case` | Evaluation input — `Input`, `Output`, `Expected`, `Context`, `Metadata`. |
| `Result` | Score output — `Score`, `Passed`, `Reason`, `Latency`, `Tokens`. |
| `Runner` | Ties Judge + Metric + Case together, asserts via `testing.TB`. Shared across parallel subtests. |

Metrics: `Faithfulness`, `Hallucination`, `AnswerRelevancy`, `ContextPrecision`, `GEval`, `Precheck`, `Compound`, `Deterministic` (JSONPath, FieldCount).

## CI / Hooks

- `.github/workflows/ci.yml` — runs on PR + push to main: `go test -race` and `golangci-lint` (v2, action `@v9`).
- `.githooks/pre-push` — runs tests + lint before every push. Configured via `core.hooksPath`.
- `.golangci.yml` — v2 config. Enabled: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `nilerr`, `gofmt`, `goimports`.

## Conventions

- `Judge` implementations must be safe for concurrent use (shared across `t.Parallel`).
- Metrics are stateless value types; config is read-only during `Score`.
- `Result.Metric` and `Result.Latency` are filled by Runner if empty/zero.
- Low scores call `tb.Errorf` (non-fatal); judge errors call `tb.Fatalf` (fatal).
- `adapters/` directory contains external integrations (e.g. OpenAI judge).
