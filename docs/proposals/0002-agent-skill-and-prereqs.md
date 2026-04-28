# Proposal: Agent Skill for Authoring `go-eval` Suites

Status: Draft
Tracking issue: #8
Target release: TBD
Created: 2026-04-24

## Summary

Ship an agentskills.io-compliant skill, `authoring-go-eval-suites`, that
teaches LLM coding agents (Claude Code, Codex, OpenCode, Cursor) how to
author, run, and review `go-eval` suites against a Go project's AI flows.

Pair the skill with a small, focused set of library hooks and conventions that
let it report rich, structured information back to the calling agent: per-case
scores, dimensions, latency, token usage, regression vs prior runs, borderline
cases, and prioritized recommendations.

The skill ships as one main document with companion references and one
slash command (`/eval`) that infers mode (design / run / review) from repo
state.

## Motivation

`go-eval` is small enough that an LLM coding agent can usefully author and
run an eval suite for a Go project without human hand-holding, given the
right reference material. Today an agent has to read the README, source,
and existing tests to figure out which metric fits which problem, what
goes in `Case.Metadata`, how to wire `GOEVAL_RESULTS_DIR`, and how to
interpret the resulting JSONL. That is a lot of context for a recurring
task.

Two things change with a dedicated skill:

1. The agent picks up a Go repo using `go-eval` and immediately knows the
   workflow shape (design → run → review), the metric selector, the
   threshold-tuning rules, and the report schema. No re-derivation per
   conversation.
2. After running the suite, the agent produces a structured report rich
   enough to drive concrete next-step recommendations, not just a
   pass/fail tally.

Remote `main` already has the core reporting foundation the skill needs:
`Result.Metadata`, `RunResult.Metadata`, JSONL result sinks, and
`GOEVAL_TRACE`. The remaining gaps are narrower: token accounting, tiered case
selection, and a shared metadata convention for coverage/reporting.

## Goals

- An LLM coding agent picking up a `go-eval` project can author a quality
  eval suite for the project's AI flows without re-reading library source.
- After running the suite, the agent produces a structured report rich
  enough to drive concrete next-step recommendations (which thresholds to
  tune, which cases to add, which flows are uncovered).
- The same skill content works across agents that follow the
  agentskills.io spec, from a single canonical source.
- Auto-loaded skill footprint stays under ~1.5k tokens; deeper reference
  material loads on demand.
- Library prereqs are small, additive, and backward-compatible.

## Non-goals

- Not a tutorial on LLM evaluation theory. The skill assumes the agent
  already understands what an eval is and why.
- Not a replacement for `AGENTS.md`. The skill is task-specific (authoring
  /running/reviewing evals); `AGENTS.md` covers repo-wide conventions.
- No agentskills.io plugin distribution mechanism in this release. Repo-
  local files only.
- No HTML or web dashboard reporting. Markdown report consumed by the
  agent.
- No auto-threshold tuning. Suggest manually only.

## Proposed Design

### Existing Foundation on `main`

The skill can rely on these existing pieces:

- `Case.Metadata`, copied into `Result.Metadata` by `Runner.Run` unless the
  metric explicitly sets `Result.Metadata`.
- `RunResult.Metadata`, emitted by JSONL result sinks.
- `Result.Dimensions` and `RunResult.Dimensions` for `Compound` output.
- `GOEVAL_RESULTS_DIR` for local JSONL persistence.
- `GOEVAL_TRACE` for judge prompt/response inspection while running evals.
- `Bench` reporting `tokens/op`, `score_mean`, and `score_stddev`.

### Proposed Library Prereqs

These ship as small, separate changes (or the first commits of the skill PR) so
the skill can report more precisely without depending on ad hoc conventions in
each consuming repo.

#### Token split in result types

```go
type JudgeResponse struct {
	Score            float64
	Reason           string
	Tokens           int // total, kept for compatibility
	PromptTokens     int
	CompletionTokens int
}

type RawJudgeResponse struct {
	Content          string
	Tokens           int
	PromptTokens     int
	CompletionTokens int
}

type Result struct {
	Score            float64
	Reason           string
	Passed           bool
	Metric           string
	Latency          time.Duration
	Tokens           int
	PromptTokens     int
	CompletionTokens int
	Dimensions       []DimensionResult
	Metadata         map[string]any
	_                struct{}
}

type RunResult struct {
	Timestamp        string            `json:"timestamp"`
	TestName         string            `json:"test_name"`
	Metric           string            `json:"metric"`
	Score            float64           `json:"score"`
	Passed           bool              `json:"passed"`
	Reason           string            `json:"reason"`
	Tokens           int               `json:"tokens"`
	PromptTokens     int               `json:"prompt_tokens,omitempty"`
	CompletionTokens int               `json:"completion_tokens,omitempty"`
	LatencyNS        int64             `json:"latency_ns"`
	Dimensions       []DimensionResult `json:"dimensions,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
}
```

The implementation updates every result propagation path:

- `metric_support.go` copies `JudgeResponse.Tokens`, `PromptTokens`, and
  `CompletionTokens` into `Result`.
- `compound.go` accumulates split token counts across `RawJudgeResponse`
  attempts and passes them through `buildCompoundResult`.
- `precheck.go` adds prompt/completion token counts from `Pre` and `Main`, the
  same way it already aggregates total `Tokens`.
- `deterministic.go` keeps all token fields at zero because no judge call is
  made.
- `eval.go` preserves metric-provided token fields when filling Runner-owned
  fields such as `Metric`, `Latency`, and fallback metadata.
- `result_sink.go` copies the split fields from `Result` to `RunResult`; JSONL
  rows include `prompt_tokens` and `completion_tokens` when non-zero.

The OpenAI adapter populates `RawJudgeResponse.Tokens`,
`RawJudgeResponse.PromptTokens`, and `RawJudgeResponse.CompletionTokens` from
`resp.Usage.TotalTokens`, `resp.Usage.PromptTokens`, and
`resp.Usage.CompletionTokens`. `Evaluate` then copies those fields into
`JudgeResponse`, including retry totals when a stricter JSON-only retry is
needed.

`Tokens` keeps its current meaning. `PromptTokens` and `CompletionTokens`
default to `0` when the judge does not report them. The report degrades
gracefully when only `Tokens` is set.

#### `WithCaseFilter` Runner option

```go
func WithCaseFilter(pred func(Case) bool) Option
```

When set, `Runner.Run` skips cases where `pred` returns false (the test
logs `"skipped by case filter"`). Enables tiered CI runs: critical-only
fast path, full nightly. Pairs with the metadata convention below.

#### Metadata convention

Add a section to `AGENTS.md` documenting these conventional `Case.Metadata`
keys:

| Key | Type | Purpose |
|---|---|---|
| `flow` | string | Logical agent flow exercised (e.g. `rag.retrieval`, `tool_use.search`). Used by the report's coverage heuristic. |
| `tier` | string | `critical` / `standard` / `extended`. Used by `WithCaseFilter` for tiered runs. |
| `dataset` | string | Provenance: dataset name + version. Used to group cases in the report. |

The library already carries metadata through results, but it does not interpret
these keys. Convention only; the skill prescribes them.

#### Deferred to follow-up changes

- `eval.RepeatN(n int, m Metric) Metric` — flakiness/variance.
- `WithTokenBudget(int)` / `WithLatencyBudget(time.Duration)` — suite-level
  guardrails.
- `compare` subpackage — already on the v0.3 roadmap; fills the report's
  Regression section when available.
- `adapters/pricing` model→price map — defer until token split lands and
  real demand exists; static price tables rot.

### Skill Layout

Canonical location is neutral so any agent can consume it. Per-agent
directories use symlinks.

```text
docs/agent-skills/
  authoring-go-eval-suites/
    SKILL.md
    references/
      metrics-reference.md
      report-template.md
      recommendations.md
    assets/
      templates/
        rag-suite.go.tmpl
        compound-suite.go.tmpl
.claude/
  skills/
    authoring-go-eval-suites/  ->  ../../docs/agent-skills/authoring-go-eval-suites/
  commands/
    eval.md
```

Future agents (Codex `~/.agents/skills/`, OpenCode, Cursor `.cursor/rules/`)
get added as additional symlinks or one-time copy steps. Cursor needs
format conversion (`.mdc` instead of `.md`); covered when the first Cursor
user requests it.

#### `SKILL.md` structure

Frontmatter (agentskills.io spec):

```yaml
---
name: authoring-go-eval-suites
description: Use when writing, running, or reviewing LLM evaluation suites with go-eval — authoring Metrics/Cases/Judges, interpreting Runner results, diagnosing failing scores, or tuning thresholds
---
```

Body sections, in order:

1. **Overview** — one-line core principle: tests encode what the agent
   *should* do; evals encode what it *does*. Both gated by `GOEVAL`.
2. **When to use / not** — symptom list. Not for agent code itself, only
   for evaluating agent output quality.
3. **Decision tree** — small flowchart: authoring vs running vs reviewing.
   Each branch points at a companion file or `/eval` mode.
4. **Quick reference table** — `Case`, `Metric`, `Judge`, `Runner`,
   `Result`, `DimensionResult` with one-line purpose and key fields.
5. **Metric selector** — symptom → metric mapping. RAG faithfulness →
   `Faithfulness`. Multi-dimension single judge call → `Compound`. Binary
   format → `Contains` / `Regex` / `JSONPath` / `FieldCount`. Custom rubric
   → `GEval`. Cheap pre-filter → `Precheck` wrapper.
6. **Authoring checklist** — seven steps: define case shape, pick metric,
   set threshold (with justification), wire judge (concurrency-safe), gate
   on `GOEVAL`, use bundled templates when useful, attach to `_test.go`.
   References `assets/templates/`.
7. **Running** — commands table covering plain, with sink, with
   `GOEVAL_TRACE`, with bench, with case filter.
8. **Reading results** — points at `references/report-template.md` and
   `references/recommendations.md`.
9. **Common mistakes** — stateful metric configs, unsafe judges, missing
   `GOEVAL` gate, threshold copied without justification, `Faithfulness`
   case missing `Context`, comparing scores across judge model changes.
10. **Red flags** — running without sink (no regression baseline), judge
    not concurrency-safe under `t.Parallel`, lowering threshold without
    3+ historical runs to justify.

Target body length: under 500 words. Heavy reference goes in companions.

#### `references/metrics-reference.md`

One section per metric. Each section:

- Required `Case` fields.
- Optional fields the metric reads.
- Score formula (one line).
- Default threshold and rationale.
- When to use, when not.
- Common anti-patterns specific to that metric.
- Pointer to the relevant prompt template in `prompts/`.

Loaded only when the skill follows a pointer (authoring or diagnosing a
specific metric).

#### `references/report-template.md`

Fixed schema the agent fills after `/eval run` or `/eval review`. The
schema must degrade gracefully when data is missing (no sink → omit
Regression; no token split → omit Cost; no `Compound` metrics → omit
Dimensions).

```text
## Eval Suite Report

Suite:          <test name pattern or pkg>
Run:            <timestamp> (commit <sha>)
Environment:    GOEVAL=<n>, trace=<bool>, sink=<path>, judge=<adapter/model>

## Scores
Cases:          <passed>/<total> (<pct>%)
Mean:           <score> ± <stddev>
Per metric:
  <metric>:     pass=<n>/<n>, mean=<n>, p95=<n>

## Dimensions (Compound only)
  <dim>:        pass=<n>/<n>, mean=<n>, threshold=<n>

## Budget
Total tokens:   <n> (prompt=<n>, completion=<n>)
Avg/case:       <n>
Total latency:  <ms> (p50=<n>, p95=<n>, p99=<n>)
Est. cost:      $<n> at <model>/<price/Mtok>     [if pricing available]

## Failures
<test>:         <metric>=<score> < <threshold>
  reason:       <judge reason>
  input:        <trim to 120ch>
  dimensions:   [<failed dims if Compound>]

## Borderline (|score - threshold| <= 0.05)
<test>: <metric>=<n>, threshold=<n>, margin=<n>

## Regression vs <prior-run>
Pass rate:      <+/-pct>
Mean score:     <+/-n>
New failures:   <list>
Newly passing:  <list>

## Coverage (heuristic)
Agent flows declared (Case.Metadata["flow"]):  <set>
Flows in agent code (best-effort scan):        <set>
Uncovered flows:                                <list>

## Recommendations
1. <prioritized list — see recommendations.md for heuristics>
```

Section ordering is fixed so agents can extract sections deterministically.

#### `references/recommendations.md`

Heuristics the skill applies when filling the Recommendations section.
Ordered by priority:

1. **Failures with consistent reasons across cases** — root cause is in
   agent code or prompt, not threshold. Recommend agent-side fix, not
   threshold adjustment.
2. **Borderline cases (margin <= 0.05) trending down across runs** — flag
   as regressing; recommend repro and investigation before threshold move.
3. **Threshold moves** — only suggest lowering when 3+ historical runs
   show consistent passing scores below the current threshold AND the
   lower bound would still catch the failure modes the metric is meant to
   catch. Never lower based on a single run.
4. **Coverage gaps** — flows present in agent code but absent from
   `Case.Metadata["flow"]` set → suggest specific case skeletons.
5. **Token budget concerns** — if avg tokens/case exceeds a configurable
   threshold or total cost crosses a soft limit, suggest the `Precheck`
   wrapper or moving to a cheaper judge for high-volume metrics.
6. **Flakiness signal** — if a test name has stddev > 0.1 across runs in
   `results.jsonl`, recommend `eval.RepeatN` (when available) or
   investigate judge non-determinism (temperature, prompt sensitivity).
7. **Compound dimension imbalance** — one dimension consistently scoring
   much lower → suggest splitting into a dedicated metric or adjusting the
   compound prompt's emphasis.

Each heuristic specifies its data source (single run / regression diff /
historical sink) so it degrades cleanly when data is missing.

#### `assets/templates/`

Two complete templates, adaptable not generic:

- `rag-suite.go.tmpl` — `Faithfulness` + `Hallucination` +
  `AnswerRelevancy` on a hand-written `Case`, table-driven subtests,
  `t.Parallel`, sink wired, metadata fields set. Uses `adapters/openai`.
- `compound-suite.go.tmpl` — single `Compound` metric with three
  dimensions, showing `DimensionResult` consumption.

One excellent example each.

### Slash Command: `/eval`

Single command, mode inferred from repo state.

`.claude/commands/eval.md` body (sketch):

```text
Invoke the `authoring-go-eval-suites` skill. Detect mode:

- If $ARGUMENTS starts with `design`, `run`, or `review`, use that mode.
- Else if no `*_eval_test.go` exists in the target package → mode = design.
- Else if `results.jsonl` does not exist or is older than the test files
  → mode = run.
- Else → mode = review.

Modes:

- design — read AGENTS.md, scan agent code for flows, propose Case set and
  metric selection, draft `_eval_test.go` per the skill's authoring
  checklist.
- run — execute `GOEVAL=1 GOEVAL_RESULTS_DIR=.eval-results/ go test -run
  Eval ./...`, parse `results.jsonl`, fill
  `references/report-template.md`.
- review — read existing `results.jsonl` (and prior runs if present), fill
  the report, surface recommendations from `references/recommendations.md`.

Always write the report to stdout. If --save <path> provided, also write
to path. If a prior report exists at the same path, include the
Regression section.
```

Specialization (`/eval-design`, `/eval-run`, `/eval-review`) deferred
until mode inference proves confusing in practice. One command keeps the
mental model simple and avoids activation collision.

## Compatibility

- Token split fields are additive on `JudgeResponse`, `RawJudgeResponse`,
  `Result`, and `RunResult`. Existing code reading `Tokens` continues to
  work. `Tokens` keeps its current meaning (total, when set).
- `WithCaseFilter` is a new `Option`; default behavior (no filter) is
  unchanged.
- Metadata convention is documentation-only and reserves three string keys; no
  runtime change. Metadata propagation itself already exists on `main`.
- `GOEVAL_TRACE` already exists on `main`; the skill documents how to use it
  but does not change trace behavior.
- The skill itself is a pure documentation artifact under `docs/`; the
  `.claude/` symlinks and `commands/` file are tooling for Claude Code
  users and have no effect on builds, tests, or library consumers.

## Risks

- **Skill rot.** The skill cites concrete library symbols and conventions;
  if the API drifts the skill misleads agents. Mitigation: skill is
  in-tree; the proposal/changelog process surfaces drift; companion files
  are short and focused so updates are localized.
- **Coverage heuristic is brittle.** It depends on string-matching
  `Case.Metadata["flow"]` against grep'd code. Will produce false
  positives and false negatives. Documented as best-effort; a real
  coverage hook is a possible future change.
- **Static pricing tables rot.** Deferring `adapters/pricing` until the
  token split lands and real demand exists; report omits the Cost section
  cleanly when no pricing source is available.
- **Cross-agent format divergence.** Cursor's `.mdc` format differs from
  agentskills.io markdown; portability across agents is not free. Not
  shipped in this proposal.

## Alternatives Considered

- **Three skills** (`design`, `run`, `review`) — rejected. The three
  stages share the same domain model (`Case` / `Metric` / `Judge` /
  `Runner`); splitting causes activation collision on overlapping
  triggers and forces each skill to re-establish context. One skill with
  an internal decision tree is cleaner.
- **Three slash commands instead of one** — deferred. Mode inference
  starting from one `/eval` is simpler; specialize only if friction
  emerges in practice.
- **Inline the skill in `AGENTS.md`** — rejected. `AGENTS.md` is repo-
  wide; the skill is task-specific and benefits from agentskills.io
  conventions (description-driven activation, on-demand companion
  references).
- **Distribute as an installable agentskills.io plugin** — deferred.
  Higher effort (plugin manifest, versioning, distribution); repo-local
  ship is the minimum viable.
- **Rich JSON or HTML report** — rejected for this release. Markdown is
  the lingua franca for agent consumption; structured machine-readable
  reports can come later if needed.

## Open Questions

- Should `/eval design` modify files or only propose? Proposing-only is
  safer; modifying is more useful. Tentative default: print the proposal,
  write the file only when invoked with `--write`.
- Where does the report go by default? Stdout for piping; optionally
  `.eval-results/report.md` when `--save` is passed. Worth standardizing
  before ship.
- Coverage heuristic depends on string-matching `Case.Metadata["flow"]`
  against grep'd code. Brittle. Worth investing in a real coverage
  mechanism later (e.g. agent code annotates flows via build tag or doc
  comment)?
- Pressure-test the skill per `superpowers:writing-skills` (subagent
  baseline → skill → close loopholes), or rely on a single live dry-run
  against `examples/rag_app/` since this is a technique skill rather than
  a discipline skill?

## Implementation Plan

1. **PR 1 — library prereqs.** Token split fields on `JudgeResponse`,
   `RawJudgeResponse`, `Result`, `RunResult`. OpenAI adapter populates
   them. `WithCaseFilter` Runner option. Metadata convention section in
   `AGENTS.md`. Tests + CHANGELOG entry. Existing metadata propagation and
   `GOEVAL_TRACE` need no new implementation. Estimated ~2 hours.
2. **PR 2 — skill + slash command.** `docs/agent-skills/...` content,
   `.claude/skills/` symlink, `.claude/commands/eval.md`, README link to
   the skill. No new library code. Estimated ~1 day.
3. **Smoke test.** Run `/eval` against `examples/rag_app/` (and one
   external sandbox repo if available). Iterate on the report template
   and recommendations until the output drives real next steps, not just
   structurally complete fields.
4. **v0.3 dovetail.** The `compare` subpackage from the v0.3 evaluation-
   core proposal fills the Regression section. `RepeatN` and budget
   options follow when demand justifies.
