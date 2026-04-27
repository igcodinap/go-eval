# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- `RunResult.Metadata` in JSONL result sinks, copied from `Case.Metadata` by default
- Split token counts (`PromptTokens`, `CompletionTokens`) on judge responses, results, and JSONL sink rows
- `WithCaseFilter` runner option for skipping cases by metadata or custom predicates

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
