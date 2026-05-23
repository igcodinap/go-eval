# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Agent Scenario Contracts: `Scenario`, `Step`, `StepRequest`, `StepFunc`,
  `StepResult`, `ScenarioResult`, and `Runner.RunScenario`
- Scenario-scoped `ToolRegistry` with `NewToolRegistry` validation
- `RequiredTools` trajectory metric and `ArtifactArrayMinLen` artifact metric
- Result sink redaction with `WithRedactors`, `UUIDRedactor`, and
  `FieldRedactor`
- Agent scenario example covering multi-step route planning contracts

## [v0.6.0] - 2026-05-22

### Added

- v0.5 trajectory primitives: `Turn`, `ToolCall`, `Case.Turns`, and
  `Case.ExpectedToolCalls`
- JSON dataset support for optional `turns` and `expected_tool_calls` fields
- v0.6 trajectory match modes: `MatchStrict`, `MatchUnordered`, `MatchSubset`,
  and `MatchSuperset`
- Trajectory metrics: `ToolCallAccuracy`, `ToolCallF1`, `ForbiddenTool`, and
  `StepBudget`
- `Repeat` and `RepeatN` for repeated metric runs and pass-rate aggregation
- Single-run result summaries through `compare.Summarize`,
  `compare.SummarizeFile`, and `goeval summarize`

## [v0.4.0] - 2026-05-22

### Added

- `Case.Artifacts` for named structured JSON outputs alongside text output
- Artifact metrics: `ArtifactExists`, `ArtifactJSONPath`, `ArtifactFieldCount`, `ArtifactNumberLTE`, and `ArtifactArrayContains`
- `WithTokenBudget` and `WithLatencyBudget` metric wrappers
- `compare.CaseIDFromMetadata` helper for stable `case_id` result comparisons
- Route planner example showing artifact-first agent workflow checks

### Changed

- `Case` now includes a private blank field, so external callers must use keyed
  struct literals such as `eval.Case{Input: "..."}`

## [v0.3.0] - 2026-04-29

### Added

- `RunResult.Metadata` in JSONL result sinks, copied from `Case.Metadata` by default
- Split token counts (`PromptTokens`, `CompletionTokens`) on judge responses, results, and JSONL sink rows
- `WithCaseFilter` runner option for skipping cases by metadata or custom predicates
- `authoring-go-eval-suites` agent skill and Claude `/eval` command for designing, running, and reviewing eval suites
- `compare` package for baseline-vs-current JSONL result regression diffs
- Minimal `goeval` CLI with `test`, `compare`, and `version` commands
- JSON dataset loader (`LoadCases`, `LoadNamedCases`, `LoadDataset`) for external golden cases
- Getting Started guide covering local judges, OpenAI, metrics, JSONL results, and benchmarks
- Ollama judge adapter (`adapters/ollama`) for local HTTP API scoring

## [v0.2.0] - 2026-04-22

### Added

- `Compound` metric for multi-dimension evaluation
- `Deterministic` metrics: `JSONPath` and `FieldCount`
- OpenAI judge adapter (`adapters/openai/`)
- `ResultSink` for persisting evaluation results to JSONL
- `Precheck` metric wrapper for conditional evaluation
- `json_text.go` helpers: `StripMarkdownCodeFence`, `ExtractJSONObjectCandidate`
- CI workflow (`.github/workflows/ci.yml`) with `go test -race` and `golangci-lint` on PR/push
- Pre-push hook (`.githooks/pre-push`) enforcing tests + lint before every push
- `AGENTS.md` with repo-specific agent instructions

## [v0.1.0] - 2026-04-21

### Added

- Core metrics: `Faithfulness`, `Hallucination`, `AnswerRelevancy`, `ContextPrecision`, `GEval`
- `Runner` with `GOEVAL` environment gate
- `Judge` and `Metric` interfaces
- `Case` and `Result` types
- `Bench` helper for benchmarking evals
- `JudgeMock` for testing without an LLM
