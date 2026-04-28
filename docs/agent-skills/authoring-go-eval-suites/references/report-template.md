# Eval Suite Report Template

Fill this report after `/eval run` or `/eval review`. Keep section order stable. Omit sections whose data is unavailable, and say why in one short line when the omission affects interpretation.

```text
## Eval Suite Report

Suite:          <test name pattern or package>
Run:            <timestamp> (commit <sha>)
Environment:    GOEVAL=<n>, trace=<bool>, sink=<path>, judge=<adapter/model>

## Scores
Cases:          <passed>/<total> (<pct>%)
Mean:           <score> +/- <stddev>
Per metric:
  <metric>:     pass=<n>/<n>, mean=<n>, p95=<n>

## Dimensions (Compound only)
  <dimension>:  pass=<n>/<n>, mean=<n>, threshold=<n>

## Budget
Total tokens:   <n> (prompt=<n>, completion=<n>)
Avg/case:       <n>
Total latency:  <ms> (p50=<n>, p95=<n>, p99=<n>)
Est. cost:      $<n> at <model>/<price per Mtok> [if pricing is available]

## Failures
<test>:         <metric>=<score> < <threshold>
  reason:       <judge reason>
  input:        <trim to 120 chars>
  dimensions:   <failed dimensions if Compound>

## Borderline
<test>:         <metric>=<score>, threshold=<n>, margin=<abs(score-threshold)>

## Regression vs <prior-run>
Pass rate:      <+/-pct>
Mean score:     <+/-n>
New failures:   <list>
Newly passing:  <list>

## Coverage
Agent flows declared in Case.Metadata["flow"]:  <set>
Flows found in agent code, best effort:         <set>
Uncovered flows:                                <list>

## Recommendations
1. <prioritized list from recommendations.md>
```

## Data Sources

- Scores, dimensions, latency, tokens, metadata: `results.jsonl` from `GOEVAL_RESULTS_DIR`.
- Commit: `git rev-parse --short HEAD`.
- Prior run: previous JSONL/report at the requested `--save` path, when present.
- Coverage: declared `Case.Metadata["flow"]` values plus best-effort source scan for agent/tool/retrieval flow names.
- Thresholds: metric constructors in eval test files when present; otherwise infer from failure output and call out uncertainty.

## Borderline Rule

Treat a score as borderline when `abs(score - threshold) <= 0.05`. Borderline passes still deserve attention when trending downward across historical results.
