package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
	"github.com/igcodinap/go-eval/internal/runstore"
)

const runsUsage = `Usage:
  goeval runs list [--limit <n>] [--runs-dir <dir>] [--json]
  goeval runs show <run|latest> [--runs-dir <dir>] [--json]
  goeval runs summary <run|latest> [--runs-dir <dir>] [--json]
  goeval runs failures <run|latest> [--runs-dir <dir>] [--json]
  goeval runs trace <run|latest> [--failed] [--trace-id <id>] [--runs-dir <dir>] [--json]
  goeval runs compare <baseline|previous> <current|latest> [--runs-dir <dir>] [--json]
  goeval runs report <run|latest> [--out <path>] [--open] [--runs-dir <dir>]
  goeval runs prune --keep <n> [--dry-run] [--runs-dir <dir>]
`

var openStoredReport = defaultOpenPath

type storedRun struct {
	ID       string
	Dir      string
	Manifest eval.RunManifest
}

type runOverview struct {
	ID         string                  `json:"id"`
	RunName    string                  `json:"run_name,omitempty"`
	Branch     string                  `json:"branch,omitempty"`
	Commit     string                  `json:"commit,omitempty"`
	Profile    string                  `json:"profile,omitempty"`
	Status     string                  `json:"status,omitempty"`
	ExitCode   int                     `json:"exit_code,omitempty"`
	PassRate   float64                 `json:"pass_rate,omitempty"`
	Failed     int                     `json:"failed,omitempty"`
	Total      int                     `json:"total,omitempty"`
	DurationNS int64                   `json:"duration_ns,omitempty"`
	StartedAt  string                  `json:"started_at,omitempty"`
	EndedAt    string                  `json:"ended_at,omitempty"`
	Command    []string                `json:"command,omitempty"`
	Artifacts  map[string]string       `json:"artifacts,omitempty"`
	Summary    *compare.ResultsSummary `json:"summary,omitempty"`
}

func runRuns(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		writeString(stdout, runsUsage)
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		writeString(stdout, runsUsage)
		return 0
	case "list":
		return runRunsList(args[1:], stdout, stderr)
	case "show":
		return runRunsShow(args[1:], stdout, stderr)
	case "summary":
		return runRunsSummary(args[1:], stdout, stderr)
	case "failures":
		return runRunsFailures(args[1:], stdout, stderr)
	case "trace":
		return runRunsTrace(args[1:], stdout, stderr)
	case "compare":
		return runRunsCompare(args[1:], stdout, stderr)
	case "report":
		return runRunsReport(args[1:], stdout, stderr)
	case "prune":
		return runRunsPrune(args[1:], stdout, stderr)
	default:
		writef(stderr, "runs: unknown command %q\n\n%s", args[0], runsUsage)
		return 2
	}
}

func runRunsList(args []string, stdout io.Writer, stderr io.Writer) int {
	limit := 20
	var runsDir string
	jsonOut := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--limit":
			i++
			if i >= len(args) {
				writef(stderr, "runs list: --limit requires a value\n")
				return 2
			}
			value, err := strconv.Atoi(args[i])
			if err != nil || value < 0 {
				writef(stderr, "runs list: --limit must be non-negative\n")
				return 2
			}
			limit = value
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil || value < 0 {
				writef(stderr, "runs list: --limit must be non-negative\n")
				return 2
			}
			limit = value
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "runs list: --runs-dir requires a value\n")
				return 2
			}
			runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--json":
			jsonOut = true
		default:
			writef(stderr, "runs list: unknown argument %q\n", arg)
			return 2
		}
	}
	store, err := runsStore(runsDir)
	if err != nil {
		writef(stderr, "runs list: %v\n", err)
		return 1
	}
	records, err := store.Records()
	if err != nil {
		writef(stderr, "runs list: %v\n", err)
		return 1
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	overviews := make([]runOverview, 0, len(records))
	for _, record := range records {
		overview, err := overviewForRun(store, record.ID, false)
		if err != nil {
			continue
		}
		overviews = append(overviews, overview)
	}
	if jsonOut {
		return writeJSON(stdout, stderr, "runs list", overviews)
	}
	for _, overview := range overviews {
		writef(
			stdout,
			"run\tid=%s\tstatus=%s\tbranch=%s\tcommit=%s\tprofile=%s\tpass_rate=%.3f\tfailed=%d\tduration_ns=%d\tstarted_at=%s\n",
			overview.ID,
			overview.Status,
			overview.Branch,
			overview.Commit,
			overview.Profile,
			overview.PassRate,
			overview.Failed,
			overview.DurationNS,
			overview.StartedAt,
		)
	}
	return 0
}

func runRunsShow(args []string, stdout io.Writer, stderr io.Writer) int {
	ref, runsDir, jsonOut, ok := parseRunRefCommand("runs show", args, stderr)
	if !ok {
		return 2
	}
	store, run, err := loadStoredRun(runsDir, ref)
	if err != nil {
		writef(stderr, "runs show: %v\n", err)
		return 1
	}
	_ = store
	overview, err := overviewForLoadedRun(run, true)
	if err != nil {
		writef(stderr, "runs show: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, "runs show", overview)
	}
	printRunOverview(stdout, overview)
	if overview.Summary != nil {
		printSummaryReport(stdout, *overview.Summary)
	}
	printSlowAndTokenHeavy(stdout, run)
	return 0
}

func runRunsSummary(args []string, stdout io.Writer, stderr io.Writer) int {
	ref, runsDir, jsonOut, ok := parseRunRefCommand("runs summary", args, stderr)
	if !ok {
		return 2
	}
	_, run, err := loadStoredRun(runsDir, ref)
	if err != nil {
		writef(stderr, "runs summary: %v\n", err)
		return 1
	}
	summary, err := summarizeStoredRun(run)
	if err != nil {
		writef(stderr, "runs summary: %v\n", err)
		return 1
	}
	if jsonOut {
		return writeJSON(stdout, stderr, "runs summary", summary)
	}
	printSummaryReport(stdout, summary)
	return 0
}

func runRunsFailures(args []string, stdout io.Writer, stderr io.Writer) int {
	ref, runsDir, jsonOut, ok := parseRunRefCommand("runs failures", args, stderr)
	if !ok {
		return 2
	}
	_, run, err := loadStoredRun(runsDir, ref)
	if err != nil {
		writef(stderr, "runs failures: %v\n", err)
		return 1
	}
	results, err := readStoredResults(run)
	if err != nil {
		writef(stderr, "runs failures: %v\n", err)
		return 1
	}
	failures := failedResults(results)
	if jsonOut {
		return writeJSON(stdout, stderr, "runs failures", failures)
	}
	for _, result := range failures {
		writeString(stdout, "failure")
		writef(stdout, "\ttest=%s", safeField(result.TestName))
		if caseID := metadataValue(result.Metadata, compare.DefaultCaseIDMetadataKey); caseID != "" {
			writef(stdout, "\tcase=%s", safeField(caseID))
		}
		writef(stdout, "\tmetric=%s\tscore=%.3f\tpassed=%t", safeField(result.Metric), result.Score, result.Passed)
		if result.TraceID != "" {
			writef(stdout, "\ttrace_id=%s", safeField(result.TraceID))
		}
		for _, key := range []string{"flow", "tier", "dataset"} {
			if value := metadataValue(result.Metadata, key); value != "" {
				writef(stdout, "\t%s=%s", key, safeField(value))
			}
		}
		if result.Reason != "" {
			writef(stdout, "\treason=%s", safeField(result.Reason))
		}
		writeln(stdout)
	}
	return 0
}

func runRunsTrace(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed := struct {
		ref     string
		runsDir string
		jsonOut bool
		failed  bool
		traceID string
	}{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "runs trace: --runs-dir requires a value\n")
				return 2
			}
			parsed.runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			parsed.runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--json":
			parsed.jsonOut = true
		case arg == "--failed":
			parsed.failed = true
		case arg == "--trace-id":
			i++
			if i >= len(args) {
				writef(stderr, "runs trace: --trace-id requires a value\n")
				return 2
			}
			parsed.traceID = args[i]
		case strings.HasPrefix(arg, "--trace-id="):
			parsed.traceID = strings.TrimPrefix(arg, "--trace-id=")
		case strings.HasPrefix(arg, "-"):
			writef(stderr, "runs trace: unknown flag %q\n", arg)
			return 2
		default:
			if parsed.ref != "" {
				writef(stderr, "runs trace: usage: goeval runs trace <run|latest>\n")
				return 2
			}
			parsed.ref = arg
		}
	}
	if parsed.ref == "" {
		writef(stderr, "runs trace: usage: goeval runs trace <run|latest>\n")
		return 2
	}
	_, run, err := loadStoredRun(parsed.runsDir, parsed.ref)
	if err != nil {
		writef(stderr, "runs trace: %v\n", err)
		return 1
	}
	tracesPath := artifactPath(run, run.Manifest.TracesPath, "traces.jsonl")
	if !fileExists(tracesPath) {
		if parsed.jsonOut {
			return writeJSON(stdout, stderr, "runs trace", []eval.Trace{})
		}
		writef(stdout, "no traces for run %s\n", run.ID)
		return 0
	}
	traces, err := eval.ReadTraceJSONLFile(tracesPath)
	if err != nil {
		writef(stderr, "runs trace: %v\n", err)
		return 1
	}
	if parsed.failed {
		traces = filterFailedTraces(run, traces)
	}
	if parsed.traceID != "" {
		traces = filterTraceID(traces, parsed.traceID)
	}
	if parsed.jsonOut {
		return writeJSON(stdout, stderr, "runs trace", traces)
	}
	for _, trace := range traces {
		printTrace(stdout, trace)
	}
	return 0
}

func runRunsCompare(args []string, stdout io.Writer, stderr io.Writer) int {
	var runsDir string
	jsonOut := false
	var refs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "runs compare: --runs-dir requires a value\n")
				return 2
			}
			runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--json":
			jsonOut = true
		case strings.HasPrefix(arg, "-"):
			writef(stderr, "runs compare: unknown flag %q\n", arg)
			return 2
		default:
			refs = append(refs, arg)
		}
	}
	if len(refs) != 2 {
		writef(stderr, "runs compare: usage: goeval runs compare <baseline|previous> <current|latest>\n")
		return 2
	}
	_, baseline, err := loadStoredRun(runsDir, refs[0])
	if err != nil {
		writef(stderr, "runs compare: baseline: %v\n", err)
		return 1
	}
	_, current, err := loadStoredRun(runsDir, refs[1])
	if err != nil {
		writef(stderr, "runs compare: current: %v\n", err)
		return 1
	}
	report, err := compare.CompareFiles(
		artifactPath(baseline, baseline.Manifest.ResultsPath, "results.jsonl"),
		artifactPath(current, current.Manifest.ResultsPath, "results.jsonl"),
	)
	if err != nil {
		writef(stderr, "runs compare: %v\n", err)
		return 1
	}
	if jsonOut {
		if code := writeJSON(stdout, stderr, "runs compare", report); code != 0 {
			return code
		}
	} else {
		printCompareReport(stdout, report)
	}
	if report.Summary.Regressed > 0 || report.Summary.Missing > 0 {
		return 1
	}
	return 0
}

func runRunsReport(args []string, stdout io.Writer, stderr io.Writer) int {
	var runsDir string
	var outPath string
	open := false
	var ref string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "runs report: --runs-dir requires a value\n")
				return 2
			}
			runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--out":
			i++
			if i >= len(args) {
				writef(stderr, "runs report: --out requires a value\n")
				return 2
			}
			outPath = args[i]
		case strings.HasPrefix(arg, "--out="):
			outPath = strings.TrimPrefix(arg, "--out=")
		case arg == "--open":
			open = true
		case strings.HasPrefix(arg, "-"):
			writef(stderr, "runs report: unknown flag %q\n", arg)
			return 2
		default:
			if ref != "" {
				writef(stderr, "runs report: usage: goeval runs report <run|latest>\n")
				return 2
			}
			ref = arg
		}
	}
	if ref == "" {
		writef(stderr, "runs report: usage: goeval runs report <run|latest>\n")
		return 2
	}
	_, run, err := loadStoredRun(runsDir, ref)
	if err != nil {
		writef(stderr, "runs report: %v\n", err)
		return 1
	}
	reportPath := artifactPath(run, run.Manifest.ReportPath, "report.html")
	if outPath != "" {
		if err := generateStoredReport(run, outPath); err != nil {
			writef(stderr, "runs report: %v\n", err)
			return 1
		}
		reportPath = outPath
	} else if !fileExists(reportPath) {
		if err := generateStoredReport(run, reportPath); err != nil {
			writef(stderr, "runs report: %v\n", err)
			return 1
		}
	}
	if open {
		if err := openStoredReport(reportPath); err != nil {
			writef(stderr, "runs report: open %s: %v\n", reportPath, err)
			return 1
		}
	}
	writef(stdout, "%s\n", reportPath)
	return 0
}

func runRunsPrune(args []string, stdout io.Writer, stderr io.Writer) int {
	var runsDir string
	dryRun := false
	keep := -1
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "runs prune: --runs-dir requires a value\n")
				return 2
			}
			runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--dry-run":
			dryRun = true
		case arg == "--keep":
			i++
			if i >= len(args) {
				writef(stderr, "runs prune: --keep requires a value\n")
				return 2
			}
			value, err := strconv.Atoi(args[i])
			if err != nil {
				writef(stderr, "runs prune: --keep must be a positive integer\n")
				return 2
			}
			keep = value
		case strings.HasPrefix(arg, "--keep="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep="))
			if err != nil {
				writef(stderr, "runs prune: --keep must be a positive integer\n")
				return 2
			}
			keep = value
		default:
			writef(stderr, "runs prune: unknown argument %q\n", arg)
			return 2
		}
	}
	if keep <= 0 {
		writef(stderr, "runs prune: --keep must be greater than zero\n")
		return 2
	}
	store, err := runsStore(runsDir)
	if err != nil {
		writef(stderr, "runs prune: %v\n", err)
		return 1
	}
	records, err := store.Records()
	if err != nil {
		writef(stderr, "runs prune: %v\n", err)
		return 1
	}
	keepIDs := map[string]bool{}
	for i, record := range records {
		if i < keep {
			keepIDs[record.ID] = true
		}
	}
	if latest, err := store.ReadLatest(); err == nil && latest != "" {
		keepIDs[latest] = true
	}
	for _, record := range records {
		if keepIDs[record.ID] {
			continue
		}
		writef(stdout, "prune\tid=%s\tpath=%s\n", record.ID, store.RunDir(record.ID))
		if !dryRun {
			if err := os.RemoveAll(store.RunDir(record.ID)); err != nil {
				writef(stderr, "runs prune: remove %s: %v\n", record.ID, err)
				return 1
			}
		}
	}
	if !dryRun {
		remaining, err := store.Scan()
		if err != nil {
			writef(stderr, "runs prune: rescan: %v\n", err)
			return 1
		}
		if err := store.WriteIndex(remaining); err != nil {
			writef(stderr, "runs prune: write index: %v\n", err)
			return 1
		}
		if len(remaining) > 0 {
			_ = store.WriteLatest(remaining[0].ID)
		}
	}
	return 0
}

func parseRunRefCommand(name string, args []string, stderr io.Writer) (string, string, bool, bool) {
	var ref string
	var runsDir string
	jsonOut := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				writef(stderr, "%s: --runs-dir requires a value\n", name)
				return "", "", false, false
			}
			runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--json":
			jsonOut = true
		case strings.HasPrefix(arg, "-"):
			writef(stderr, "%s: unknown flag %q\n", name, arg)
			return "", "", false, false
		default:
			if ref != "" {
				writef(stderr, "%s: usage: goeval %s <run|latest>\n", name, name)
				return "", "", false, false
			}
			ref = arg
		}
	}
	if ref == "" {
		writef(stderr, "%s: usage: goeval %s <run|latest>\n", name, name)
		return "", "", false, false
	}
	return ref, runsDir, jsonOut, true
}

func runsStore(runsDir string) (runstore.Store, error) {
	root, err := resolveRunsRoot(runsDir)
	if err != nil {
		return runstore.Store{}, err
	}
	return runstore.New(root), nil
}

func loadStoredRun(runsDir string, ref string) (runstore.Store, storedRun, error) {
	store, err := runsStore(runsDir)
	if err != nil {
		return runstore.Store{}, storedRun{}, err
	}
	id, err := store.Resolve(ref)
	if err != nil {
		return runstore.Store{}, storedRun{}, err
	}
	manifest, err := eval.ReadRunManifest(store.ManifestPath(id))
	if err != nil {
		return runstore.Store{}, storedRun{}, err
	}
	dir := store.RunDir(id)
	if manifest.RunID != "" {
		id = manifest.RunID
	}
	return store, storedRun{ID: id, Dir: dir, Manifest: manifest}, nil
}

func overviewForRun(store runstore.Store, id string, includeSummary bool) (runOverview, error) {
	manifest, err := eval.ReadRunManifest(store.ManifestPath(id))
	if err != nil {
		return runOverview{}, err
	}
	dir := store.RunDir(id)
	if manifest.RunID != "" {
		id = manifest.RunID
	}
	return overviewForLoadedRun(storedRun{ID: id, Dir: dir, Manifest: manifest}, includeSummary)
}

func overviewForLoadedRun(run storedRun, includeSummary bool) (runOverview, error) {
	overview := runOverview{
		ID:         run.ID,
		RunName:    run.Manifest.RunName,
		Branch:     run.Manifest.Branch,
		Commit:     shortString(run.Manifest.Commit, 7),
		Profile:    run.Manifest.Profile,
		Status:     run.Manifest.Status,
		ExitCode:   run.Manifest.ExitCode,
		DurationNS: run.Manifest.DurationNS,
		StartedAt:  run.Manifest.StartedAt,
		EndedAt:    run.Manifest.EndedAt,
		Command:    append([]string(nil), run.Manifest.Command...),
		Artifacts: map[string]string{
			"manifest":     filepath.Join(run.Dir, eval.RunManifestFileName),
			"results":      artifactPath(run, run.Manifest.ResultsPath, "results.jsonl"),
			"traces":       artifactPath(run, run.Manifest.TracesPath, "traces.jsonl"),
			"judge_events": artifactPath(run, run.Manifest.JudgeEventsPath, "judge-events.jsonl"),
			"test_events":  artifactPath(run, run.Manifest.TestEventsPath, "test-events.jsonl"),
			"summary":      artifactPath(run, run.Manifest.SummaryPath, "summary.json"),
			"report":       artifactPath(run, run.Manifest.ReportPath, "report.html"),
		},
	}
	summary, err := summarizeStoredRun(run)
	if err == nil {
		overview.PassRate = summary.PassRate
		overview.Failed = summary.Failed
		overview.Total = summary.Total
		if includeSummary {
			overview.Summary = &summary
		}
	}
	return overview, nil
}

func summarizeStoredRun(run storedRun) (compare.ResultsSummary, error) {
	return compare.SummarizeFile(artifactPath(run, run.Manifest.ResultsPath, "results.jsonl"))
}

func readStoredResults(run storedRun) ([]eval.RunResult, error) {
	path := artifactPath(run, run.Manifest.ResultsPath, "results.jsonl")
	if !fileExists(path) {
		return nil, fmt.Errorf("results file not found: %s", path)
	}
	return compare.ReadJSONLFile(path)
}

func artifactPath(run storedRun, path string, fallback string) string {
	if path == "" {
		return filepath.Join(run.Dir, fallback)
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(run.Dir, path)
}

func printRunOverview(w io.Writer, overview runOverview) {
	writef(w, "Run: %s\n", overview.ID)
	writef(w, "Status: %s exit_code=%d\n", overview.Status, overview.ExitCode)
	if len(overview.Command) > 0 {
		writef(w, "Command: %s\n", strings.Join(overview.Command, " "))
	}
	if overview.Branch != "" || overview.Commit != "" {
		writef(w, "Git: branch=%s commit=%s\n", overview.Branch, overview.Commit)
	}
	writef(w, "Started: %s\nEnded: %s\nDurationNS: %d\n", overview.StartedAt, overview.EndedAt, overview.DurationNS)
	keys := make([]string, 0, len(overview.Artifacts))
	for key := range overview.Artifacts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writef(w, "Artifact: %s=%s\n", key, overview.Artifacts[key])
	}
}

func printSlowAndTokenHeavy(w io.Writer, run storedRun) {
	results, err := readStoredResults(run)
	if err != nil {
		return
	}
	nonSummary := results[:0]
	for _, result := range results {
		if result.Kind == "scenario_summary" || result.Metric == "_scenario_summary" {
			continue
		}
		nonSummary = append(nonSummary, result)
	}
	sort.SliceStable(nonSummary, func(i int, j int) bool {
		return nonSummary[i].LatencyNS > nonSummary[j].LatencyNS
	})
	for i := 0; i < len(nonSummary) && i < 5; i++ {
		result := nonSummary[i]
		writef(w, "slow\tlatency_ns=%d\ttest=%s\tmetric=%s\n", result.LatencyNS, safeField(result.TestName), safeField(result.Metric))
	}
	sort.SliceStable(nonSummary, func(i int, j int) bool {
		return nonSummary[i].Tokens > nonSummary[j].Tokens
	})
	for i := 0; i < len(nonSummary) && i < 5; i++ {
		result := nonSummary[i]
		writef(w, "tokens\ttokens=%d\ttest=%s\tmetric=%s\n", result.Tokens, safeField(result.TestName), safeField(result.Metric))
	}
}

func failedResults(results []eval.RunResult) []eval.RunResult {
	failures := make([]eval.RunResult, 0)
	for _, result := range results {
		if result.Kind == "scenario_summary" || result.Metric == "_scenario_summary" {
			continue
		}
		if !result.Passed {
			failures = append(failures, result)
		}
	}
	return failures
}

func filterFailedTraces(run storedRun, traces []eval.Trace) []eval.Trace {
	results, err := readStoredResults(run)
	if err != nil {
		return traces[:0]
	}
	failedIDs := map[string]bool{}
	for _, result := range failedResults(results) {
		if result.TraceID != "" {
			failedIDs[result.TraceID] = true
		}
	}
	filtered := traces[:0]
	for _, trace := range traces {
		if failedIDs[trace.ID] {
			filtered = append(filtered, trace)
		}
	}
	return filtered
}

func filterTraceID(traces []eval.Trace, id string) []eval.Trace {
	filtered := traces[:0]
	for _, trace := range traces {
		if trace.ID == id {
			filtered = append(filtered, trace)
		}
	}
	return filtered
}

func printTrace(w io.Writer, trace eval.Trace) {
	writef(w, "trace\tid=%s\tname=%s\ttest=%s\tduration_ns=%d\n", safeField(trace.ID), safeField(trace.Name), safeField(trace.TestName), trace.DurationNS)
	children := map[string][]eval.Span{}
	for _, span := range trace.Spans {
		children[span.ParentID] = append(children[span.ParentID], span)
	}
	printSpanChildren(w, children, "", 1)
	for _, artifact := range trace.Artifacts {
		writef(w, "  artifact\tkey=%s\tname=%s\tmime=%s\n", safeField(artifact.Key), safeField(artifact.Name), safeField(artifact.MIMEType))
	}
	for _, delta := range trace.StateDeltas {
		writef(w, "  state\tkey=%s\n", safeField(delta.Key))
	}
}

func printSpanChildren(w io.Writer, children map[string][]eval.Span, parent string, depth int) {
	for _, span := range children[parent] {
		indent := strings.Repeat("  ", depth)
		writef(w, "%sspan\tid=%s\tname=%s\tkind=%s\tduration_ns=%d", indent, safeField(span.ID), safeField(span.Name), safeField(span.Kind), span.DurationNS)
		if span.Error != "" {
			writef(w, "\terror=%s", safeField(span.Error))
		}
		if span.ToolCall != nil {
			writef(w, "\ttool=%s", safeField(span.ToolCall.Name))
		}
		writeln(w)
		if span.ID != "" {
			printSpanChildren(w, children, span.ID, depth+1)
		}
	}
}

func generateStoredReport(run storedRun, outPath string) error {
	resultsPath := artifactPath(run, run.Manifest.ResultsPath, "results.jsonl")
	summary, err := compare.SummarizeFile(resultsPath)
	if err != nil {
		return err
	}
	report, err := compare.ReportHTML(compare.NewResultsReport(resultsPath, summary))
	if err != nil {
		return err
	}
	return runstore.WriteFileAtomic(outPath, report, 0o644)
}

func defaultOpenPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func writeJSON(stdout io.Writer, stderr io.Writer, label string, value any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		writef(stderr, "%s: encode json: %v\n", label, err)
		return 1
	}
	return 0
}

func metadataValue(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func safeField(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}

func shortString(value string, n int) string {
	if n <= 0 || len(value) <= n {
		return value
	}
	return value[:n]
}
