# Recommendation Heuristics

Apply these in order when filling the report. Prefer concrete next actions over generic advice.

## 1. Repeated Failure Reasons

Data source: single run or historical sink.

If multiple cases fail with the same judge reason, recommend fixing agent code, prompts, retrieval, or tool orchestration before moving thresholds. Repeated reasons usually mean a real behavior gap.

## 2. Borderline Downtrends

Data source: historical sink or prior report.

If borderline cases are trending down, recommend reproducing the change and inspecting recent prompt/model/code changes. Do not call it stable just because it still passes.

## 3. Threshold Moves

Data source: at least three historical runs.

Only suggest lowering a threshold when three or more comparable runs show stable scores below the current threshold and the new threshold would still catch the intended failure mode. Never lower from a single run. Mention judge model changes as a blocker to comparison.

## 4. Coverage Gaps

Data source: `Case.Metadata["flow"]` values plus best-effort source scan.

When a flow appears in code but not in eval metadata, propose a named case skeleton with `flow`, `tier`, and `dataset` filled in. Prioritize critical user-facing flows.

## 5. Token Or Latency Budget

Data source: JSONL token and latency fields.

If average tokens or p95 latency is high, recommend `Precheck`, a smaller judge for high-volume metrics, `WithCaseFilter` for critical-only paths, or reducing overlapping metrics.

## 6. Flakiness Signal

Data source: repeated runs or benchmark output.

If the same case has score standard deviation above `0.1`, recommend reducing judge temperature, tightening rubrics, adding examples, or repeating the metric when a repeat helper becomes available.

## 7. Compound Imbalance

Data source: `DimensionResult` rows.

If one dimension consistently scores lower than the others, recommend splitting it into a dedicated metric or clarifying that dimension's rubric. If all dimensions drop together, look for an upstream agent behavior change.

## Output Rules

- Start with fixes that change product behavior, then eval authoring changes, then budget optimizations.
- Include the test or flow name behind every recommendation.
- Do not recommend deleting hard cases just because they fail.
- When data is missing, say which command or metadata would unlock the stronger recommendation.
