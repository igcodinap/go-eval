# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

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
