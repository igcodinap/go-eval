# Proposal: Local Observability Run Store

Status: Implemented
Tracking issue: #TBD
Target release: v1.2.0
Created: 2026-07-05

## Summary

Add an explicit local observability workflow around `goeval test --store`.

The feature creates a local run store under `.goeval/`, persists one directory
per run, and adds `goeval runs ...` commands for listing, inspecting,
summarizing, comparing, reporting, and pruning those runs. It builds on the
existing `go test` execution model, `GOEVAL_RESULTS_DIR`, JSONL result sinks,
trace sinks, judge-event sinks, run manifests, and `compare` package.

The core package remains a stdlib-only evaluation library. Normal `go test`
and direct `Runner.Run` behavior do not silently write history. The `goeval`
CLI is the opinionated local observability entrypoint.

## Motivation

Modern evaluation and observability tools converge on a local-first shape:
structured run artifacts, queryable summaries, regression comparisons, and
optional later upload or dashboard views.

`go-eval` already has most of the raw artifact contracts:

- `results.jsonl` from `GOEVAL_RESULTS_DIR`
- `traces.jsonl` from the trace sink
- `judge-events.jsonl` from the judge executor event sink
- `goeval-run.json` as a manifest sidecar
- `compare.Summarize`, `compare.Compare`, and static reports

The missing layer is a durable local run store and a CLI explorer that makes
those artifacts easy to browse after a test run.

## Goals

- Add a local `.goeval/` run store that is explicit, predictable, and safe to
  ignore in normal Go workflows.
- Make `goeval test --store` the default way to run evals with local
  observability artifacts.
- Preserve current `goeval test` behavior when `--store` is not used.
- Preserve stored-run human stdout as much as possible by re-rendering
  `go test -json` output events, while storing the raw event stream.
- Preserve stderr behavior and `go test` exit codes.
- Capture `go test -json` events into `test-events.jsonl`.
- Generate a run manifest, summary JSON, and static HTML report for stored
  runs.
- Add a small `goeval runs` explorer for the common local and CI workflows.
- Keep all new implementation stdlib-only.
- Keep existing public APIs and artifact readers backward compatible.

## Non-goals

- No hosted product or required cloud account.
- No background daemon.
- No automatic history writes from `Runner.Run`.
- No SQL database in the first version.
- No TUI in the first version.
- No OpenTelemetry export in the first version.
- No upload or web server command until the local store format stabilizes.

## Proposed Design

### Run Store Layout

The default run store lives under `.goeval/`:

```text
.goeval/
  index.json
  latest
  runs/
    2026-07-05T223012Z-main-a1b2c3d/
      goeval-run.json
      results.jsonl
      traces.jsonl
      judge-events.jsonl
      test-events.jsonl
      summary.json
      report.html
```

`latest` is a small text file containing the latest run ID. It is not a symlink,
so the store works consistently on platforms and filesystems where symlinks are
awkward.

`index.json` is a convenience cache for fast listing. Commands should tolerate
missing or stale index data by reading manifests from `.goeval/runs/*` when
needed.

### Store Root

The default store root is the current module root's `.goeval/` directory. The
CLI should find the module root by walking up to `go.mod`, falling back to the
current working directory if no module root is found.

`--runs-dir` overrides the default store root. Relative `--runs-dir` values are
resolved against the current working directory because the user supplied an
explicit path.

Every `goeval runs ...` subcommand accepts `--runs-dir`; otherwise aliases such
as `latest` and `previous` would only work for the default store.

### Run IDs

Default run IDs use:

```text
<utc-timestamp>-<branch>-<short-commit>
```

Example:

```text
2026-07-05T223012Z-main-a1b2c3d
```

Rules:

- sanitize generated branch-derived segments and validate user-provided IDs as
  already path-safe
- append a numeric suffix if a run ID already exists
- allow `--run-id` for deterministic CI workflows
- allow `--run-name` as human-readable manifest metadata, not as the primary
  stable ID
- error when a user-provided `--run-id` already exists, rather than silently
  overwriting or suffixing it
- sort runs by manifest `started_at` when resolving `latest` and `previous`,
  not by run ID, so custom run IDs do not break alias behavior

Generated run IDs should use second-level UTC timestamps plus collision
suffixes. That keeps IDs readable while still handling repeated local runs:

```text
2026-07-05T223012Z-main-a1b2c3d
2026-07-05T223012Z-main-a1b2c3d-2
```

### Manifest Contract

Extend `RunManifest` additively. Existing manifest readers must continue to
accept v1.1.0 manifests.

New fields:

```go
type RunManifest struct {
	RunID          string   `json:"run_id,omitempty"`
	RunName        string   `json:"run_name,omitempty"`
	Repo           string   `json:"repo,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	Commit         string   `json:"commit,omitempty"`
	ExitCode       int      `json:"exit_code,omitempty"`
	Status         string   `json:"status,omitempty"`
	TestEventsPath string   `json:"test_events_path,omitempty"`
	SummaryPath    string   `json:"summary_path,omitempty"`
	ReportPath     string   `json:"report_path,omitempty"`
}
```

`Status` is one of:

- `passed`
- `failed`
- `error`

Status mapping:

- `passed`: `go test` exits with code `0` and required stored-run artifacts are
  written successfully.
- `failed`: `go test` exits non-zero and the parsed test-event stream contains
  at least one test-level failure event.
- `error`: the `go` command cannot start, the event stream cannot be captured,
  no usable test-event stream is available for a non-zero exit, manifest writing
  fails, or tests passed but required stored-run artifact generation failed.

Build failures, package setup failures, and timeouts that do not produce a
test-level failure event are treated as `error`. If tests fail and optional
summary or report generation also fails, the manifest should keep
`status="failed"` and record the artifact-generation error in `metadata`.

The existing fields remain canonical for schema versions, command, profile,
packages, artifact paths, start/end time, duration, and metadata.

All new fields use `omitempty`. Pre-v1.2 manifests remain valid; new fields
decode to zero values.

### Internal Run Store Package

Add `internal/runstore` for CLI-only storage mechanics.

Responsibilities:

- create store directories
- create and sanitize generated run IDs
- write and read `index.json`
- update `latest`
- resolve aliases: `latest`, `previous`, and explicit run IDs
- load run bundles from disk
- prune old run directories
- compute default repo metadata from Git when available
- write `latest`, `index.json`, and manifests atomically with temp files and
  rename
- keep `index.json` disposable: read commands must work by scanning manifests
  when the index is missing, corrupt, or stale

This package remains internal because it is storage plumbing for the CLI, not
yet a public API contract.

The first version does not need a cross-process lock. Concurrent stored runs
must not corrupt JSON files because writes use temp-file-and-rename, but
`latest` is last-writer-wins. Read commands recover by scanning manifests and
rewriting stale index data when practical.

### `goeval test --store`

Extend the existing `goeval test` command:

```bash
goeval test --store ./...
goeval test --store --profile pr
goeval test --store --runs-dir .goeval ./...
goeval test --store --run-id ci-123 ./...
goeval test --store --run-name "pr smoke" ./...
```

Behavior:

1. Create a run directory under the selected runs dir.
2. Set `GOEVAL=1`.
3. Set `GOEVAL_RESULTS_DIR` to the run directory, overriding inherited
   `GOEVAL_RESULTS_DIR` and profile `results_dir` only for stored mode.
4. Preserve profile package, tier, and prerequisite behavior from
   `goeval.json`.
5. Force `go test -json` unless the forwarded args already include `-json`.
6. Run `go test`.
7. Write raw JSON event lines to `test-events.jsonl`.
8. Re-render valid event `Output` fields to the user's stdout.
9. Preserve malformed stdout lines by writing them through to stdout and
   recording a manifest diagnostic.
10. Preserve stderr behavior.
11. Preserve the `go test` exit code.
12. Write `goeval-run.json` including the exit code and status.
13. If `results.jsonl` exists, write `summary.json`.
14. If `results.jsonl` exists, write `report.html`.
15. Update `.goeval/latest` and `.goeval/index.json` after manifest and
   required post-run artifacts are complete.

The command should not fail a run only because optional summary or report
generation failed after a non-zero test exit. It should record the diagnostic
error in the manifest metadata and preserve the test exit code. If tests pass
but artifact generation fails, return a non-zero CLI code because the requested
stored run is incomplete.

If the process is interrupted after the run directory is created, the directory
is kept for debugging. `latest` should only point at runs with a written
manifest.

`test-events.jsonl` stores the `go test -json` stream verbatim. The CLI should
parse the stream incrementally using a small internal event shape:

```go
type testEvent struct {
	Time    string  `json:"Time,omitempty"`
	Action  string  `json:"Action"`
	Package string  `json:"Package,omitempty"`
	Test    string  `json:"Test,omitempty"`
	Elapsed float64 `json:"Elapsed,omitempty"`
	Output  string  `json:"Output,omitempty"`
}
```

The parsed events are diagnostics only; they do not replace `results.jsonl` as
the canonical eval-result artifact.

### `goeval runs list`

List recent runs:

```bash
goeval runs list
goeval runs list --limit 20
goeval runs list --runs-dir .goeval
goeval runs list --json
```

Default text columns:

- run id
- branch
- commit
- profile
- status
- pass rate
- failures
- duration
- started at

### `goeval runs show`

Show one run overview:

```bash
goeval runs show latest
goeval runs show 2026-07-05T223012Z-main-a1b2c3d
goeval runs show latest --json
goeval runs show latest --runs-dir .goeval
```

Include:

- command
- exit code and status
- package/profile metadata
- result totals and pass rate
- metric summaries
- failed case count
- slowest cases
- token-heavy cases
- artifact paths

### `goeval runs summary`

Render the existing summary view for a run:

```bash
goeval runs summary latest
goeval runs summary latest --json
goeval runs summary latest --runs-dir .goeval
```

Text output should reuse the existing summary formatting so users do not have
to learn a second summary vocabulary.

### `goeval runs failures`

Focused failed-case view:

```bash
goeval runs failures latest
goeval runs failures latest --json
goeval runs failures latest --runs-dir .goeval
```

Rows include:

- test name
- case name when present
- metric
- score
- threshold/pass state when available
- reason
- trace ID
- flow/tier/dataset metadata when present

### `goeval runs trace`

Render traces from `traces.jsonl`:

```bash
goeval runs trace latest
goeval runs trace latest --failed
goeval runs trace latest --trace-id <id>
goeval runs trace latest --runs-dir .goeval
```

The first version should be a readable text tree:

- trace name and ID
- span hierarchy using `Span.ParentID`
- span kind/name/duration/error
- tool call names and arguments summarized
- artifact keys
- state delta keys

If no trace file exists, return a clear message and success for text output,
or an empty JSON structure for `--json`.

### `goeval runs compare`

Compare stored runs:

```bash
goeval runs compare previous latest
goeval runs compare <baseline-run-id> <current-run-id>
goeval runs compare previous latest --json
goeval runs compare previous latest --runs-dir .goeval
```

Behavior:

- resolve aliases with `internal/runstore`
- compare each run's `results.jsonl`
- reuse existing `compare.CompareFiles`
- preserve the existing compare failure semantics for regressions

Policy flags and automatic `goeval.json` compare-policy loading are deferred so
the first stored-run comparison matches the current `goeval compare` behavior.

### `goeval runs report`

Open or print the static report path:

```bash
goeval runs report latest
goeval runs report latest --out report.html
goeval runs report latest --open
goeval runs report latest --runs-dir .goeval
```

Default behavior prints the path to `report.html`, generating it if missing and
`results.jsonl` exists.

`--open` may use the OS opener when available. Tests should isolate this behind
a small function so no GUI is launched during unit tests.

### `goeval runs prune`

Manage disk growth:

```bash
goeval runs prune --keep 20
goeval runs prune --keep 20 --dry-run
goeval runs prune --keep 20 --runs-dir .goeval
```

Rules:

- keep newest N runs by manifest start time, falling back to directory mod time
- never remove the run pointed to by `latest` unless it is also outside the
  keep set and the user supplied an explicit future `--force` flag
- update `index.json` after pruning
- reject `--keep 0` in the first version

## Compatibility

- Existing `RunManifest` JSON remains readable because all fields are additive.
- Existing `results.jsonl`, `traces.jsonl`, and `judge-events.jsonl` formats do
  not change.
- Direct `Runner.Run` continues to write only when configured with sinks or
  `GOEVAL_RESULTS_DIR`.
- `goeval test` without `--store` keeps its current behavior.
- `goeval test --store` is the only path that writes `.goeval/`.
- The root package remains stdlib-only; CLI and internal packages also use only
  stdlib for this version.

## Risks

- Forcing `go test -json` changes stdout shape for `goeval test --store`. This
  is acceptable because `--store` is explicit, but the CLI should re-render
  `Output` fields so the user sees readable test output.
- Test output teeing can accidentally buffer too much if implemented naively.
  It should stream line by line and write raw event lines to `test-events.jsonl`.
- `index.json` can become stale if users delete run directories manually.
  Commands should recover by scanning manifests.
- Failed runs may lack `results.jsonl`. Explorer commands must handle missing
  optional files gracefully.
- Git metadata collection can fail outside Git repos. Store creation should
  continue with empty repo fields.
- Concurrent stored runs can race on `latest` and `index.json`. Atomic writes
  prevent corruption, but `latest` remains last-writer-wins in v1.2.0.

## Alternatives Considered

### Silent Runner History

Rejected. It would make ordinary `go test` surprising and could create local
history in repos that only intended an in-memory test run.

### SQL Store

Deferred. SQL would make querying easier, but the current library already
speaks JSONL and manifest files. Files keep the first version inspectable,
portable, and easy to upload later.

### TUI First

Deferred. A text and JSON CLI is easier to test, useful in CI, and agent
friendly.

### Public Run Store API

Deferred. The first version should stabilize the on-disk bundle and CLI
behavior before committing to a public Go API.

### Raw JSON Stdout in Stored Mode

Rejected for the default behavior. Stored mode needs `go test -json`, but the
CLI can preserve a human-readable stream by re-emitting event `Output` fields
while writing raw events to `test-events.jsonl`.

## Decisions

- Add `.goeval/` to this repo's `.gitignore` during implementation and show
  the same ignore guidance in docs for consuming repos.
- Always force `go test -json` in stored mode.
- Do not add a raw-output escape hatch in v1.2.0.
- Keep `runs compare` aligned with current `goeval compare` default behavior;
  policy flags can follow later.
- Keep v1.2.0 `report.html` focused on eval `results.jsonl`; test-event
  failure sections can follow in a richer report update.

## Implementation Plan

0. Freeze compatibility fixtures.
   - pre-v1.2 manifest fixture
   - current `goeval test` no-store arg/env behavior
   - existing compare/report output expectations

1. Add `internal/runstore`.
   - path helpers
   - run ID creation and sanitization
   - alias resolution
   - index read/write with scan fallback
   - bundle loading
   - prune selection
   - atomic file writes

2. Extend `RunManifest` additively.
   - new run identity, repo, status, and artifact path fields
   - round-trip tests for old and current manifest JSON

3. Add stored-mode parsing and test-event helpers.
   - `--store`
   - `--runs-dir`
   - `--run-id`
   - `--run-name`
   - keep current `--profile` and `--config` behavior
   - pure status-mapping helper over parsed test events
   - streaming event capture that writes JSONL and re-renders event output

4. Implement stored test execution.
   - create run directory before invoking `go test`
   - set `GOEVAL_RESULTS_DIR`
   - force `-json`
   - write raw JSON events to `test-events.jsonl`
   - re-render event `Output` fields to stdout
   - preserve exit code and stderr

5. Generate post-run artifacts.
   - manifest with exit code/status
   - `summary.json` from `compare.SummarizeFile`
   - `report.html` from `compare.ReportHTML`
   - index/latest updates

6. Add core `goeval runs` explorer commands.
   - `list`
   - `show`
   - `summary`
   - `report`
   - `prune`
   - text output first
   - `--json` output for automation
   - graceful handling for missing optional files

7. Add result-focused diagnostic commands.
   - `failures`
   - `compare`
   - alias resolution
   - compare package reuse

8. Add `goeval runs trace`.
   - trace JSONL loading
   - failed-trace filtering via result `trace_id`
   - span tree rendering

9. Update documentation.
   - README quick workflow
   - getting-started local observability section
   - `.goeval/` ignore guidance
   - changelog under `Unreleased`

10. Verification gate.
    - `sh scripts/check-core-stdlib.sh`
    - `go test ./...`
    - `go test -race ./...`
    - `golangci-lint run`
    - nested module race tests

## Test Plan

- `internal/runstore`
  - creates collision-free run IDs
  - sanitizes generated branch names and rejects unsafe user-provided run IDs
  - errors when a user-provided `--run-id` already exists
  - resolves `latest`, `previous`, and explicit IDs
  - sorts aliases by manifest `started_at`, not run ID
  - recovers from stale or missing `index.json`
  - prunes the expected directories
  - rejects `--keep 0`
  - writes `latest` and `index.json` atomically enough to avoid corrupt files
  - resolves the default store root to the module root when possible

- `goeval test --store`
  - sets `GOEVAL=1`
  - sets `GOEVAL_RESULTS_DIR` to the run directory
  - overrides inherited `GOEVAL_RESULTS_DIR` and profile `results_dir` in
    stored mode
  - preserves profile tier env behavior
  - appends `-json` when absent
  - does not duplicate `-json` when present
  - writes `test-events.jsonl`
  - re-renders event `Output` fields to stdout instead of raw JSON
  - preserves non-zero `go test` exit codes
  - writes manifest/status for failed runs
  - classifies pass, test failure, build error, timeout, malformed event stream,
    and command-start failure
  - writes summary/report when `results.jsonl` exists
  - does not update `latest` before a manifest exists
  - keeps no-store `goeval test` behavior unchanged

- `goeval runs`
  - list/show from index and scan fallback
  - all subcommands accept `--runs-dir`
  - summary handles malformed JSONL with line-numbered errors
  - failures handles missing results as a clear command error
  - trace handles missing traces as an empty result
  - compare resolves aliases and reuses compare failure semantics
  - report regenerates missing report
  - prune supports dry-run and updates index

- Compatibility
  - pre-v1.2 manifests read successfully
  - existing result and trace JSONL golden rows still round-trip
  - `goeval test` without `--store` preserves current argument/env behavior
