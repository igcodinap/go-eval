# Metrics Reference

Use this file when choosing or diagnosing a `go-eval` metric. Metrics return normalized scores in `[0,1]`; the metric passes when the score is at least its threshold unless noted.

## Faithfulness

- Required fields: `Case.Output`, `Case.Context`.
- Optional fields: `Case.Input` for extra judge context.
- Score: judge estimate that output claims are supported by context.
- Default threshold: `0.8`, strict enough for factual RAG answers without demanding perfect wording.
- Use for: grounded generation, citations, retrieved-answer consistency.
- Avoid for: answer usefulness without source context; use `AnswerRelevancy` or `GEval`.
- Anti-pattern: testing faithfulness with empty or unrelated context.
- Prompt: `prompts/faithfulness.tmpl`.

## Hallucination

- Required fields: `Case.Output`, `Case.Context`.
- Optional fields: `Case.Input`.
- Score: judge estimate that output avoids unsupported facts.
- Default threshold: `0.9`, because invented facts are usually high-severity.
- Use for: factual safety and "do not make things up" policies.
- Avoid for: retrieved document quality; use `ContextPrecision`.
- Anti-pattern: expecting it to reward helpfulness or completeness.
- Prompt: `prompts/hallucination.tmpl`.

## AnswerRelevancy

- Required fields: `Case.Input`, `Case.Output`.
- Optional fields: `Case.Context`.
- Score: judge estimate that output addresses the input.
- Default threshold: `0.7`, allowing concise answers that omit nonessential detail.
- Use for: intent following, directness, avoiding off-topic responses.
- Avoid for: factual grounding; pair with `Faithfulness` for RAG.
- Anti-pattern: using it alone for safety-sensitive factual answers.
- Prompt: `prompts/answer_relevancy.tmpl`.

## ContextPrecision

- Required fields: `Case.Input`, `Case.Context`.
- Optional fields: `Case.Output`.
- Score: judge estimate that retrieved context is relevant to the input.
- Default threshold: `0.7`, allowing a small amount of noisy retrieval.
- Use for: retriever quality, tool search quality, RAG context selection.
- Avoid for: generation quality after retrieval; pair with answer metrics.
- Anti-pattern: hiding bad retrieval because the generator still answered correctly.
- Prompt: `prompts/context_precision.tmpl`.

## GEval

- Required fields: whatever the custom `Criteria` and optional `Steps` inspect.
- Optional fields: all `Case` fields may be referenced by the rubric.
- Score: judge applies the custom rubric.
- Default threshold: `0.7`.
- Use for: domain-specific requirements that built-in metrics do not express.
- Avoid for: simple binary checks that deterministic metrics can catch.
- Anti-pattern: vague criteria such as "answer well" without observable requirements.
- Prompt: `prompts/geval.tmpl`.

## Compound

- Required fields: fields used by each dimension.
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
