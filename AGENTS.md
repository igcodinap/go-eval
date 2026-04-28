# go-eval — Agent Instructions

## What this is

LLM evaluation library for Go. Brings LLM-as-judge metrics into `go test`. Stdlib-only core, zero external platform requirements.

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

## Task-specific agent skills

For authoring, running, or reviewing `go-eval` suites, use the canonical
agent-agnostic skill at
`docs/agent-skills/authoring-go-eval-suites/SKILL.md`.

Agent-specific entrypoints such as `.claude/commands/eval.md` should remain
thin adapters that point to the canonical skill instead of duplicating its full
workflow.

## Architecture

Core interfaces in root package:

| Type | Purpose |
|------|---------|
| `Judge` | LLM provider abstraction — sends prompt, returns `{score, reason, tokens, prompt_tokens, completion_tokens}`. Must be concurrency-safe. |
| `Metric` | Evaluation contract — `Score(ctx, judge, case) → Result`. Stateless value types. |
| `Case` | Evaluation input — `Input`, `Output`, `Expected`, `Context`, `Metadata`. |
| `Result` | Score output — `Score`, `Passed`, `Reason`, `Latency`, `Tokens`, `PromptTokens`, `CompletionTokens`. |
| `Runner` | Ties Judge + Metric + Case together, asserts via `testing.TB`. Shared across parallel subtests. |

Metrics: `Faithfulness`, `Hallucination`, `AnswerRelevancy`, `ContextPrecision`, `GEval`, `Precheck`, `Compound`, `Deterministic` (JSONPath, FieldCount).

## CI / Hooks

- `.github/workflows/ci.yml` — runs on PR + push to main: `go test -race` and `golangci-lint` (v2, action `@v9`).
- `.githooks/pre-push` — runs tests + lint before every push. Configured via `core.hooksPath`.
- `.golangci.yml` — v2 config. Enabled: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `nilerr`, `gofmt`, `goimports`.

## Releases / Changelog

- `CHANGELOG.md` at repo root — Keep a Changelog format, semver.
- Tag releases as `v<major>.<minor>.<patch>` (e.g. `v0.2.0`).
- Before tagging: update `CHANGELOG.md` with the new version, date, and grouped changes (Added, Changed, Fixed, Removed). Move entries from `## Unreleased` into the new version section.
- Tag: `git tag -a v0.x.0 -m "v0.x.0"` then `git push origin v0.x.0`.

## Conventions

- `Judge` implementations must be safe for concurrent use (shared across `t.Parallel`).
- Metrics are stateless value types; config is read-only during `Score`.
- `Result.Metric` and `Result.Latency` are filled by Runner if empty/zero.
- Low scores call `tb.Errorf` (non-fatal); judge errors call `tb.Fatalf` (fatal).
- `adapters/` directory contains external integrations (e.g. OpenAI judge).

### Case metadata conventions

`Case.Metadata` is user-defined and copied into JSONL results. The library does
not interpret these keys, but eval suites and agent reports should use them
consistently:

| Key | Type | Purpose |
|-----|------|---------|
| `flow` | string | Logical agent flow exercised (e.g. `rag.retrieval`, `tool_use.search`) |
| `tier` | string | Case selection tier: `critical`, `standard`, or `extended` |
| `dataset` | string | Dataset name and version/provenance |

## Testing env-gated behavior

- Tests that rely on an env var being **unset** must explicitly call `t.Setenv(EnvVar, "")` at the start. Never assume the inherited environment is clean — a developer shell or CI job may have exported the var globally.
- Tests that rely on an env var being **set** must call `t.Setenv(EnvVar, "1")` at the start (already standard practice).
- This applies to `GOEVAL`, `GOEVAL_TRACE`, and any future env gates.
