# /eval

Invoke the project `authoring-go-eval-suites` skill. The canonical,
agent-agnostic source is `docs/agent-skills/authoring-go-eval-suites/`; keep
this command as a thin Claude adapter.

## Mode Detection

Read `$ARGUMENTS`.

- If it starts with `design`, `run`, or `review`, use that mode.
- Else inspect the target package, or the current package when no target is given.
- Compute matching eval test files: `_test.go` files that import `github.com/igcodinap/go-eval`, call `eval.NewRunner`, or call `eval.Bench`.
- If there are no matching eval test files, mode is `design`.
- Else compare `.eval-results/results.jsonl` to the newest matching `_test.go` mtime.
- If `results.jsonl` is missing under `.eval-results/`, or older than that newest matching `_test.go`, mode is `run`.
- Else mode is `review`.

## Modes

### design

Read `AGENTS.md`, scan agent-facing code for flows, propose a case set and metric selection, and draft an eval suite from the skill checklist and `docs/agent-skills/authoring-go-eval-suites/assets/templates/`. Print the proposed file to stdout. Only write files when `$ARGUMENTS` includes `--write`.

### run

Run:

```bash
GOEVAL=1 GOEVAL_RESULTS_DIR=.eval-results go test -run Eval ./...
```

If the repository uses a different eval test pattern, adapt `-run` to the discovered tests. Parse `.eval-results/results.jsonl`, then fill `docs/agent-skills/authoring-go-eval-suites/references/report-template.md`.

If no `results.jsonl` is produced, fill only the sections supported by `go test`
output, mark Budget and Regression unavailable, and recommend adding
`eval.WithResultSink(eval.DefaultResultSink())` to the runner.

### review

Read `.eval-results/results.jsonl` and prior reports if present. Fill the report template and apply `docs/agent-skills/authoring-go-eval-suites/references/recommendations.md`.

## Output

Always write the report to stdout for `run` and `review`. If `$ARGUMENTS` includes `--save <path>`, also write the report to that path. If a prior report exists at the same path, include the Regression section.
