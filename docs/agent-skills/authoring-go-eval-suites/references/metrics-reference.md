# Metrics Reference

Use this file when choosing or diagnosing a `go-eval` metric. Metrics return normalized scores in `[0,1]`; the metric passes when the score is at least its threshold unless noted.

For LLM-judge metrics, "typically needed fields" are not hard-validated by the library. Missing fields usually degrade evaluation quality instead of failing fast.

## Faithfulness

- Typically needed fields: `Case.Output`, `Case.Context`.
- Optional fields: `Case.Input` for extra judge context.
- Score: judge estimate that output claims are supported by context.
- Default threshold: `0.8`, strict enough for factual RAG answers without demanding perfect wording.
- Use for: grounded generation, citations, retrieved-answer consistency.
- Avoid for: answer usefulness without source context; use `AnswerRelevancy` or `GEval`.
- Anti-pattern: testing faithfulness with empty or unrelated context.
- Prompt: `prompts/faithfulness.tmpl`.

## Hallucination

- Typically needed fields: `Case.Output`, `Case.Context`.
- Optional fields: `Case.Input`.
- Score: judge estimate that output avoids unsupported facts.
- Default threshold: `0.9`, because invented facts are usually high-severity.
- Use for: factual safety and "do not make things up" policies.
- Avoid for: retrieved document quality; use `ContextPrecision`.
- Anti-pattern: expecting it to reward helpfulness or completeness.
- Prompt: `prompts/hallucination.tmpl`.

## AnswerRelevancy

- Typically needed fields: `Case.Input`, `Case.Output`.
- Optional fields: `Case.Context`.
- Score: judge estimate that output addresses the input.
- Default threshold: `0.7`, allowing concise answers that omit nonessential detail.
- Use for: intent following, directness, avoiding off-topic responses.
- Avoid for: factual grounding; pair with `Faithfulness` for RAG.
- Anti-pattern: using it alone for safety-sensitive factual answers.
- Prompt: `prompts/answer_relevancy.tmpl`.

## ContextPrecision

- Typically needed fields: `Case.Input`, `Case.Context`.
- Optional fields: `Case.Output`.
- Score: judge estimate that retrieved context is relevant to the input.
- Default threshold: `0.7`, allowing a small amount of noisy retrieval.
- Use for: retriever quality, tool search quality, RAG context selection.
- Avoid for: generation quality after retrieval; pair with answer metrics.
- Anti-pattern: hiding bad retrieval because the generator still answered correctly.
- Prompt: `prompts/context_precision.tmpl`.

## GEval

- Typically needed fields: whatever the custom `Criteria` and optional `Steps` inspect.
- Optional fields: all `Case` fields may be referenced by the rubric.
- Score: judge applies the custom rubric.
- Default threshold: `0.7`.
- Use for: domain-specific requirements that built-in metrics do not express.
- Avoid for: simple binary checks that deterministic metrics can catch.
- Anti-pattern: vague criteria such as "answer well" without observable requirements.
- Prompt: `prompts/geval.tmpl`.

## Compound

- Typically needed fields: fields used by each dimension.
- Optional fields: all `Case` fields may be referenced by dimension rubrics.
- Score: aggregate of dimension scores; each `DimensionResult` has its own pass/fail.
- Default threshold: per dimension.
- Use for: related rubric dimensions that should share one judge call.
- Avoid for: unrelated failure modes that need independent analysis or thresholds.
- Anti-pattern: too many dimensions in one prompt, causing shallow judge reasoning.
- Prompt: `prompts/compound.tmpl`.

## Precheck

- Required fields: fields needed by the precheck and main metric.
- Optional fields: depends on wrapped metrics.
- Score: main metric score when precheck passes; otherwise the precheck result.
- Default threshold: inherited from wrapped metrics.
- Use for: cheap deterministic guards before expensive LLM judging.
- Avoid for: hiding failures by making the precheck too broad.
- Anti-pattern: precheck that passes almost everything and adds no budget value.

## Contains

- Required fields: `Case.Output` plus configured substring.
- Score: `1` when output contains the substring, else `0`.
- Default threshold: binary pass.
- Use for: exact required phrase, simple policy marker, fixed token.
- Avoid for: semantic equivalence.
- Anti-pattern: brittle checks against wording that the model may validly vary.

## Regex

- Required fields: `Case.Output` plus configured regex.
- Score: `1` when output matches, else `0`.
- Default threshold: binary pass.
- Use for: structured IDs, required formatting, safe response shape.
- Avoid for: parsing nested JSON; use `JSONPath`.
- Anti-pattern: overbroad patterns that accept malformed output.

## JSONPath

- Required fields: JSON `Case.Output`, configured path, expected value.
- Score: `1` when the JSON path value equals expected, else `0`.
- Default threshold: binary pass.
- Use for: structured tool output and API-like model responses.
- Avoid for: free-form text.
- Anti-pattern: using string contains checks for JSON fields.

## FieldCount

- Required fields: JSON `Case.Output`, minimum top-level field count.
- Score: `1` when enough non-null fields are present, else `0`.
- Default threshold: binary pass.
- Use for: completeness gates on structured outputs.
- Avoid for: validating field semantics; pair with `JSONPath` or `GEval`.
- Anti-pattern: rewarding many fields when the contract requires only a few exact ones.

## Artifact Checks

- Required fields: `Case.Artifacts`, configured artifact key, and any configured JSON path.
- Score: deterministic binary pass/fail, except field count and array length checks can report partial ratios.
- Metrics: `ArtifactExists`, `ArtifactJSONPath`, `ArtifactFieldCount`,
  `ArtifactNumberLTE`, `ArtifactArrayContains`, `ArtifactArrayNotContains`,
  `ArtifactArrayMinLen`, `ArtifactNotExists`, and `ArtifactSubset`.
- Matching: contains/not-contains/subset checks support `[*]` wildcard artifact
  paths and optional normalizers for case or Spanish accent folding.
  `ArtifactSubset` treats expected arrays as order-insensitive subsets.
- Use for: agent state, route plans, planner outputs, budgets, and other
  structured artifacts that should be checked before judge-backed metrics.
- Avoid for: semantic quality of final prose; pair with `GEval` or `Compound`.
- Anti-pattern: checking only that an artifact exists when the state contract
  requires fields such as `status`, `success`, or a minimum number of stops.

## ToolCallAccuracy

- Required fields: `Case.Turns`, `Case.ExpectedToolCalls`.
- Score: fraction of expected/actual tool calls matched under `Mode`.
- Default threshold: `1.0`.
- Matching: names match exactly; `MatchArgs` compares normalized JSON and treats
  missing expected arguments as a wildcard; `MatchResult` compares non-empty
  expected result strings and treats empty expected results as a wildcard.
- Use for: deterministic trajectory checks with strict, unordered, subset, or superset matching.
- Avoid for: semantic quality of final prose; pair with `Faithfulness`, `AnswerRelevancy`, or `GEval`.
- Anti-pattern: enabling `MatchArgs` with non-JSON arguments.

## ToolCallF1

- Required fields: `Case.Turns`, `Case.ExpectedToolCalls`.
- Score: F1 over matched tool calls; precision, recall, and F1 are emitted as dimensions.
- Default threshold: `0.8`.
- Matching: same name, argument, result, wildcard, and duplicate-call semantics
  as `ToolCallAccuracy`.
- Use for: tool-use suites where extras and omissions should both be visible.
- Avoid for: exact sequence assertions; use `ToolCallAccuracy{Mode: MatchStrict}`.
- Anti-pattern: relying on F1 alone when a forbidden tool is high-severity.

## RequiredTools

- Required fields: `Case.Turns`, configured `Names` or `Patterns`.
- Score: `1` when every configured tool name and pattern appears at least once, else `0`.
- Patterns use stdlib glob syntax, not regular expressions.
- Default threshold: binary pass.
- Use for: must-call behavior in tool-use or scenario steps.
- Avoid for: validating order or arguments; use `ToolCallAccuracy` when those matter.
- Anti-pattern: checking provider display names instead of stable tool names.

## ForbiddenTool

- Required fields: `Case.Turns`, configured `Names` or `Patterns`.
- Score: `1` when none of the configured tool names or non-excepted pattern
  matches appear, else `0`.
- Patterns use stdlib glob syntax, not regular expressions.
- Default threshold: binary pass.
- Use for: policy and safety gates on tool access.
- Avoid for: allow-list behavior; use `ToolCallAccuracy` or a custom deterministic metric.
- Anti-pattern: using display labels instead of stable tool names.

## StepBudget

- Required fields: `Case.Turns`, configured `MaxSteps`.
- Score: `1` when within budget; otherwise `MaxSteps / actual_steps`.
- Default threshold: binary pass.
- Use for: cheap prechecks before expensive judge metrics.
- Avoid for: counting all transcript messages; it intentionally counts tool calls only.
- Anti-pattern: setting budgets so tight that legitimate retries always fail.

## Repeat

- Required fields: wrapped metric, `N >= 2`.
- Score: mean score across repeated runs; pass/fail uses pass rate.
- Default pass rate: `1.0`.
- Use for: nondeterministic LLM judge runs and flakiness checks.
- Avoid for: deterministic metrics unless you are testing wrapper behavior.
- Anti-pattern: using repeats to hide unstable prompts instead of diagnosing them.

## Contract

- Required fields: `ContractName` and one or more checks.
- Score: mean score across checks; pass/fail requires every check to pass.
- Use for: grouped deterministic checks that should produce one named JSONL row.
- Avoid for: unrelated checks that deserve independent triage.

## Scenario Contracts

- Required fields: `Scenario.Name`, `Scenario.Driver`, and ordered `Steps`.
- Score: `ScenarioResult.Passed` is true only when all step contract/check
  results pass after any `ExpectFail` inversion.
- Use for: multi-turn agent flows with step-specific required/forbidden tools,
  tool-call budgets, accumulated artifacts, and expected negative cases.
- `ScenarioRepeat` repeats the whole scenario; `Scenario.State` and
  `StepResult.State` carry driver runtime state between steps.
- Scenario result sinks include one `_scenario_summary` row with per-step tool
  calls, emitted artifact keys, failed metrics, and repeat counts.
- Avoid for: single-turn cases where plain `Runner.Run` with a `Case` is clearer.
- Anti-pattern: putting app orchestration into custom metrics instead of using
  the scenario driver to collect turns and artifacts.
