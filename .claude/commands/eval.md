# /eval

Invoke the `authoring-go-eval-suites` skill.

## Mode Detection

Read `$ARGUMENTS`.

- If it starts with `design`, `run`, or `review`, use that mode.
- Else inspect the target package, or the current package when no target is given.
- If no `_test.go` imports `github.com/igcodinap/go-eval` and no `_test.go` uses `eval.NewRunner` or `eval.Bench`, mode is `design`.
- Else if no `results.jsonl` exists under `.eval-results/`, or it is older than eval test files, mode is `run`.
- Else mode is `review`.

## Modes

### design

Read `AGENTS.md`, scan agent-facing code for flows, propose a case set and metric selection, and draft an eval suite from the skill checklist. Print the proposed file to stdout. Only write files when `$ARGUMENTS` includes `--write`.

### run

Run:

```bash
GOEVAL=1 GOEVAL_RESULTS_DIR=.eval-results go test -run Eval ./...
```

If the repository uses a different eval test pattern, adapt `-run` to the discovered tests. Parse `.eval-results/results.jsonl`, then fill `docs/agent-skills/authoring-go-eval-suites/report-template.md`.

If no `results.jsonl` is produced, fill only the sections supported by `go test`
output, mark Budget and Regression unavailable, and recommend adding
`eval.WithResultSink(eval.DefaultResultSink())` to the runner.

### review

Read `.eval-results/results.jsonl` and prior reports if present. Fill the report template and apply `docs/agent-skills/authoring-go-eval-suites/recommendations.md`.

## Output

Always write the report to stdout for `run` and `review`. If `$ARGUMENTS` includes `--save <path>`, also write the report to that path. If a prior report exists at the same path, include the Regression section.
