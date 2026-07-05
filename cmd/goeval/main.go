package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
)

var version = "v1.2.0"

type goCommandFunc func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) int

const usage = `Usage:
  goeval test [--store] [--runs-dir <dir>] [--run-id <id>] [--run-name <name>] [--profile <name>] [--config <path>] [go test args...]
  goeval runs list|show|summary|failures|trace|compare|report|prune [...]
  goeval eval --metric contains|regex|jsonpath|field-count --dataset <cases.json> [--out <results.jsonl>]
  goeval compare [--policy <path>] [--config <path>] [--case-id-key <key>] [--score-tolerance <float>] [--fail-on-regression=<bool>] [--format text|json] <baseline.jsonl> <current.jsonl>
  goeval summarize [--policy <path>] [--config <path>] [--case-id-key <key>] <results.jsonl>
  goeval report [--format html|markdown|json] [--out <path>] <results.jsonl>
  goeval report [--format html|markdown|json] [--out <path>] --baseline <baseline.jsonl> --current <current.jsonl>
  goeval calibrate [--judge-key <key>] [--case-id-key <key>] [--pairwise-key <key>] [--score-tolerance <float>] [--format text|json] <results.jsonl>
  goeval version

Commands:
  test       Run go test with GOEVAL=1 set.
  runs       Inspect locally stored goeval test runs.
  eval       Run deterministic post-hoc evals over a dataset (jsonpath uses go-eval's limited dot-path syntax).
  compare    Compare two go-eval JSONL result files.
  summarize  Summarize one go-eval JSONL result file.
  report     Render a static report from result JSONL.
  calibrate  Summarize judge disagreement from result JSONL.
  version    Print the goeval CLI version.
`

func main() {
	code := run(context.Background(), os.Args[1:], os.Environ(), os.Stdin, os.Stdout, os.Stderr, runGoCommand)
	os.Exit(code)
}

func run(ctx context.Context, args []string, baseEnv []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, goCmd goCommandFunc) int {
	if len(args) == 0 {
		writeString(stdout, usage)
		return 0
	}

	switch args[0] {
	case "-h", "--help", "help":
		writeString(stdout, usage)
		return 0
	case "test":
		return runTest(ctx, args[1:], baseEnv, stdin, stdout, stderr, goCmd)
	case "runs":
		return runRuns(args[1:], stdout, stderr)
	case "eval":
		return runEval(ctx, args[1:], stdout, stderr)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "summarize":
		return runSummarize(args[1:], stdout, stderr)
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "calibrate":
		return runCalibrate(args[1:], stdout, stderr)
	case "version":
		return runVersion(stdout)
	default:
		writef(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runTest(
	ctx context.Context,
	args []string,
	baseEnv []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	goCmd goCommandFunc,
) int {
	if goCmd == nil {
		goCmd = runGoCommand
	}

	parsed, err := parseTestArgs(args)
	if err != nil {
		writef(stderr, "test: %v\n", err)
		return 2
	}
	if !parsed.store && (parsed.runsDir != "" || parsed.runID != "" || parsed.runName != "") {
		writef(stderr, "test: --runs-dir, --run-id, and --run-name require --store\n")
		return 2
	}
	if parsed.profile == "" {
		if parsed.configPath != "" {
			writef(stderr, "test: --config requires --profile\n")
			return 2
		}
		goArgs := append([]string{"test"}, parsed.forwarded...)
		env := setEnv(baseEnv, eval.EnvVar, "1")
		if parsed.store {
			return runGoTestStored(ctx, goArgs, env, stdin, stdout, stderr, goCmd, storedTestRequest{
				runsDir: parsed.runsDir,
				runID:   parsed.runID,
				runName: parsed.runName,
			})
		}
		resultsDir := lookupEnvValue(env, eval.ResultsDirEnvVar)
		return runGoTestWithManifest(ctx, goArgs, env, stdin, stdout, stderr, goCmd, runManifestRequest{
			resultsDir: resultsDir,
		})
	}

	manifest, _, err := loadManifest(parsed.configPath, true)
	if err != nil {
		writef(stderr, "test: read config: %v\n", err)
		return 2
	}
	profile, ok := manifest.Profiles[parsed.profile]
	if !ok {
		writef(stderr, "test: profile %q not found\n", parsed.profile)
		return 2
	}

	missing := checkManifestPrerequisites(ctx, profile.Prerequisites)
	if len(missing) > 0 {
		if profile.MissingPrerequisite == "fail" {
			printMissingPrerequisites(stderr, parsed.profile, "failed", missing)
			return 1
		}
		printMissingPrerequisites(stdout, parsed.profile, "skipped", missing)
		return 0
	}

	testArgs := append([]string{"test"}, profile.Packages...)
	testArgs = append(testArgs, parsed.forwarded...)
	if len(testArgs) == 1 {
		testArgs = append(testArgs, "./...")
	}

	env := setEnv(baseEnv, eval.EnvVar, "1")
	if len(profile.Tiers) > 0 {
		env = setEnv(env, eval.TierEnvVar, strings.Join(profile.Tiers, ","))
	} else {
		env = unsetEnv(env, eval.TierEnvVar)
	}
	if profile.ResultsDir != "" {
		env = setEnv(env, eval.ResultsDirEnvVar, profile.ResultsDir)
	} else {
		env = unsetEnv(env, eval.ResultsDirEnvVar)
	}
	if parsed.store {
		return runGoTestStored(ctx, testArgs, env, stdin, stdout, stderr, goCmd, storedTestRequest{
			profile:  parsed.profile,
			packages: append([]string(nil), profile.Packages...),
			runsDir:  parsed.runsDir,
			runID:    parsed.runID,
			runName:  parsed.runName,
		})
	}
	return runGoTestWithManifest(ctx, testArgs, env, stdin, stdout, stderr, goCmd, runManifestRequest{
		profile:    parsed.profile,
		packages:   append([]string(nil), profile.Packages...),
		resultsDir: profile.ResultsDir,
	})
}

type runManifestRequest struct {
	profile    string
	packages   []string
	resultsDir string
}

func runGoTestWithManifest(
	ctx context.Context,
	goArgs []string,
	env []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	goCmd goCommandFunc,
	req runManifestRequest,
) int {
	start := time.Now()
	code := goCmd(ctx, goArgs, env, stdin, stdout, stderr)
	if req.resultsDir == "" {
		return code
	}

	end := time.Now()
	manifest := eval.NewRunManifest()
	manifest.GoEvalVersion = version
	manifest.Command = append([]string{"go"}, goArgs...)
	manifest.Profile = req.profile
	manifest.Packages = append([]string(nil), req.packages...)
	manifest.ResultsPath = filepath.Join(req.resultsDir, "results.jsonl")
	manifest.TracesPath = filepath.Join(req.resultsDir, "traces.jsonl")
	manifest.JudgeEventsPath = filepath.Join(req.resultsDir, "judge-events.jsonl")
	manifest.StartedAt = start.UTC().Format(time.RFC3339Nano)
	manifest.EndedAt = end.UTC().Format(time.RFC3339Nano)
	manifest.DurationNS = int64(end.Sub(start))

	if err := eval.WriteRunManifest(filepath.Join(req.resultsDir, eval.RunManifestFileName), manifest); err != nil {
		writef(stderr, "test: write run manifest: %v\n", err)
		if code == 0 {
			return 1
		}
	}
	return code
}

func runGoCommand(ctx context.Context, args []string, env []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		writef(stderr, "go %s: %v\n", strings.Join(args, " "), err)
		return 1
	}
	return 0
}

func runEval(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, err := parseEvalArgs(args)
	if err != nil {
		writef(stderr, "eval: %v\n", err)
		return 2
	}

	metric, err := evalMetric(parsed)
	if err != nil {
		writef(stderr, "eval: %v\n", err)
		return 2
	}

	cases, err := eval.LoadNamedCases(parsed.datasetPath)
	if err != nil {
		writef(stderr, "eval: load dataset: %v\n", err)
		return 1
	}

	out := stdout
	var closeOut func() error
	if parsed.outPath != "" {
		if err := os.MkdirAll(filepath.Dir(parsed.outPath), 0o755); err != nil {
			writef(stderr, "eval: create output dir: %v\n", err)
			return 1
		}
		f, err := os.OpenFile(parsed.outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			writef(stderr, "eval: open output: %v\n", err)
			return 1
		}
		out = f
		closeOut = f.Close
	}

	enc := json.NewEncoder(out)
	evaluator := eval.NewEvaluator(nil)
	failed := 0
	for _, tc := range cases {
		result, err := evaluator.EvaluateNamed(ctx, tc.Name, metric, tc.Case)
		if err != nil {
			if closeOut != nil {
				_ = closeOut()
			}
			writef(stderr, "eval: %s: %v\n", tc.Name, err)
			return 1
		}
		if !result.Passed {
			failed++
		}
		if err := enc.Encode(eval.NewRunResult(tc.Name, result)); err != nil {
			if closeOut != nil {
				_ = closeOut()
			}
			writef(stderr, "eval: write result: %v\n", err)
			return 1
		}
	}
	if closeOut != nil {
		if err := closeOut(); err != nil {
			writef(stderr, "eval: close output: %v\n", err)
			return 1
		}
		writef(stdout, "wrote %s\n", parsed.outPath)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func evalMetric(parsed evalCommandArgs) (eval.Metric, error) {
	switch parsed.metric {
	case "contains":
		return eval.Contains{}, nil
	case "regex":
		if parsed.pattern == "" {
			return nil, errors.New("--pattern is required for --metric regex")
		}
		return eval.Regex{Pattern: parsed.pattern}, nil
	case "jsonpath":
		if parsed.path == "" {
			return nil, errors.New("--path is required for --metric jsonpath")
		}
		metric, err := eval.NewJSONPath(parsed.path)
		if err != nil {
			return nil, err
		}
		return metric, nil
	case "field-count":
		if parsed.minFields <= 0 {
			return nil, errors.New("--min-fields must be greater than zero for --metric field-count")
		}
		return eval.FieldCount{MinFields: parsed.minFields}, nil
	default:
		return nil, errors.New("--metric must be contains, regex, jsonpath, or field-count")
	}
}

func runCompare(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, err := parseCompareArgs(args)
	if err != nil {
		writef(stderr, "compare: %v\n", err)
		return 2
	}
	if parsed.baselinePath == "" || parsed.currentPath == "" {
		writef(stderr, "usage: goeval compare <baseline.jsonl> <current.jsonl>\n")
		return 2
	}

	policy, usePolicy, err := loadComparePolicy(parsed)
	if err != nil {
		writef(stderr, "compare: %v\n", err)
		return 1
	}

	var report compare.Report
	if usePolicy {
		report, err = compare.CompareFilesWithPolicy(parsed.baselinePath, parsed.currentPath, policy)
	} else {
		report, err = compare.CompareFiles(parsed.baselinePath, parsed.currentPath)
	}
	if err != nil {
		writef(stderr, "compare: %v\n", err)
		return 1
	}

	if parsed.format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			writef(stderr, "compare: encode json: %v\n", err)
			return 1
		}
	} else {
		printCompareReport(stdout, report)
	}
	if usePolicy && report.Summary.PolicyFailures > 0 {
		return 1
	}
	if !usePolicy && (report.Summary.Regressed > 0 || report.Summary.Missing > 0) {
		return 1
	}
	return 0
}

func runSummarize(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, err := parseSummarizeArgs(args)
	if err != nil {
		writef(stderr, "summarize: %v\n", err)
		return 2
	}

	policy, usePolicy, err := loadSummaryPolicy(parsed)
	if err != nil {
		writef(stderr, "summarize: %v\n", err)
		return 1
	}

	var summary compare.ResultsSummary
	if usePolicy {
		summary, err = compare.SummarizeFileWithPolicy(parsed.resultPath, policy)
	} else {
		summary, err = compare.SummarizeFile(parsed.resultPath)
	}
	if err != nil {
		writef(stderr, "summarize: %v\n", err)
		return 1
	}

	printSummaryReport(stdout, summary)
	return 0
}

func runReport(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, err := parseReportArgs(args)
	if err != nil {
		writef(stderr, "report: %v\n", err)
		return 2
	}

	format := parsed.format
	if format == "" {
		format, err = inferReportFormat(parsed.outPath)
		if err != nil {
			writef(stderr, "report: %v\n", err)
			return 2
		}
	}

	var report compare.StaticReport
	if parsed.baselinePath != "" || parsed.currentPath != "" {
		if parsed.baselinePath == "" || parsed.currentPath == "" {
			writef(stderr, "report: --baseline and --current must be used together\n")
			return 2
		}
		comparison, err := compare.CompareFiles(parsed.baselinePath, parsed.currentPath)
		if err != nil {
			writef(stderr, "report: compare: %v\n", err)
			return 1
		}
		summary, err := compare.SummarizeFile(parsed.currentPath)
		if err != nil {
			writef(stderr, "report: summarize current: %v\n", err)
			return 1
		}
		report = compare.NewComparisonReport(parsed.baselinePath, parsed.currentPath, summary, comparison)
	} else {
		summary, err := compare.SummarizeFile(parsed.resultPath)
		if err != nil {
			writef(stderr, "report: summarize: %v\n", err)
			return 1
		}
		report = compare.NewResultsReport(parsed.resultPath, summary)
	}

	rendered, err := renderStaticReport(report, format)
	if err != nil {
		writef(stderr, "report: render: %v\n", err)
		return 1
	}
	if parsed.outPath == "" {
		_, _ = stdout.Write(rendered)
		if len(rendered) == 0 || rendered[len(rendered)-1] != '\n' {
			writeln(stdout)
		}
		return 0
	}
	if err := os.WriteFile(parsed.outPath, rendered, 0o644); err != nil {
		writef(stderr, "report: write %q: %v\n", parsed.outPath, err)
		return 1
	}
	writef(stdout, "wrote %s\n", parsed.outPath)
	return 0
}

func runCalibrate(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, err := parseCalibrateArgs(args)
	if err != nil {
		writef(stderr, "calibrate: %v\n", err)
		return 2
	}

	report, err := compare.CalibrateFile(parsed.resultPath, compare.CalibrationOptions{
		CaseIDKey:      parsed.caseIDKey,
		JudgeKey:       parsed.judgeKey,
		VariantKey:     parsed.pairwiseKey,
		ScoreTolerance: parsed.scoreTolerance,
	})
	if err != nil {
		writef(stderr, "calibrate: %v\n", err)
		return 1
	}

	if parsed.format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			writef(stderr, "calibrate: encode json: %v\n", err)
			return 1
		}
		return 0
	}
	printCalibrationReport(stdout, report)
	return 0
}

func runVersion(stdout io.Writer) int {
	writef(stdout, "goeval %s\n", version)
	return 0
}

func printCompareReport(w io.Writer, report compare.Report) {
	s := report.Summary
	writef(
		w,
		"Summary: total=%d added=%d missing=%d improved=%d regressed=%d unchanged=%d policy_failures=%d\n",
		s.Total,
		s.Added,
		s.Missing,
		s.Improved,
		s.Regressed,
		s.Unchanged,
		s.PolicyFailures,
	)

	for _, entry := range report.Entries {
		if entry.Status == compare.StatusUnchanged {
			continue
		}
		printEntry(w, entry)
	}
}

func printSummaryReport(w io.Writer, summary compare.ResultsSummary) {
	writef(
		w,
		"Summary: total=%d passed=%d failed=%d pass_rate=%.3f scenarios=%d scenario_runs=%d scenario_pass_runs=%d\n",
		summary.Total,
		summary.Passed,
		summary.Failed,
		summary.PassRate,
		summary.ScenarioTotal,
		summary.ScenarioRuns,
		summary.ScenarioPassRuns,
	)

	printSummaryGroups(w, "metric", summary.ByMetric)
	printSummaryGroups(w, "tier", summary.ByTier)
	printSummaryGroups(w, "flow", summary.ByFlow)
	printSummaryGroups(w, "dataset", summary.ByDataset)
	printSummaryGroups(w, "case", summary.ByCase)
	for _, flaky := range summary.Flaky {
		printFlakySummary(w, flaky)
	}
}

func printCalibrationReport(w io.Writer, report compare.CalibrationReport) {
	writef(
		w,
		"Summary: groups=%d disagreements=%d judges=%d pairwise=%d\n",
		report.Summary.TotalGroups,
		report.Summary.DisagreementGroups,
		report.Summary.JudgeCount,
		report.Summary.PairwiseCount,
	)
	for _, disagreement := range report.Disagreements {
		writeString(w, "disagreement")
		if disagreement.Identity.TestName != "" {
			writef(w, "\ttest=%s", disagreement.Identity.TestName)
		}
		if disagreement.Identity.CaseName != "" {
			writef(w, "\tcase=%s", disagreement.Identity.CaseName)
		}
		writef(
			w,
			"\tmetric=%s\tscore_range=%.3f\tpass_disagreement=%t\tscore_disagreement=%t",
			disagreement.Identity.Metric,
			disagreement.ScoreRange,
			disagreement.PassDisagreement,
			disagreement.ScoreDisagreement,
		)
		for _, judge := range disagreement.Judges {
			writef(w, "\t%s=%.3f/%t", judge.Judge, judge.Score, judge.Passed)
		}
		writeln(w)
	}
	for _, pairwise := range report.Pairwise {
		writef(
			w,
			"pairwise\t%s_vs_%s\tcount=%d\tleft_wins=%d\tright_wins=%d\tties=%d\tmean_score_delta=%+.3f\n",
			pairwise.Left,
			pairwise.Right,
			pairwise.Count,
			pairwise.LeftWins,
			pairwise.RightWins,
			pairwise.Ties,
			pairwise.MeanScoreDelta,
		)
	}
}

func renderStaticReport(report compare.StaticReport, format string) ([]byte, error) {
	switch format {
	case "html":
		return compare.ReportHTML(report)
	case "markdown":
		return compare.ReportMarkdown(report)
	case "json":
		return compare.ReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be html, markdown, or json")
	}
}

func inferReportFormat(outPath string) (string, error) {
	switch strings.ToLower(filepath.Ext(outPath)) {
	case "":
		return "html", nil
	case ".html", ".htm":
		return "html", nil
	case ".md", ".markdown":
		return "markdown", nil
	case ".json":
		return "json", nil
	default:
		return "", errors.New("--format is required when --out extension is not .html, .htm, .md, .markdown, or .json")
	}
}

func printSummaryGroups(w io.Writer, label string, groups map[string]compare.MetricSummary) {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		printMetricSummary(w, label, key, groups[key])
	}
}

func printMetricSummary(w io.Writer, label string, key string, s compare.MetricSummary) {
	writef(
		w,
		"%s=%s\tcount=%d\tpassed=%d\tfailed=%d\tmean_score=%.3f\tstddev=%.3f\tmin_score=%.3f\tmax_score=%.3f\tmean_tokens=%.1f\tmean_latency_ns=%d\tpass_rate=%.3f\tp95_tokens=%.1f\tp95_latency_ns=%d\n",
		label,
		key,
		s.Count,
		s.Passed,
		s.Failed,
		s.MeanScore,
		s.StdDev,
		s.MinScore,
		s.MaxScore,
		s.MeanTokens,
		int64(s.MeanLatency),
		s.PassRate,
		s.P95Tokens,
		int64(s.P95Latency),
	)
}

func printFlakySummary(w io.Writer, flaky compare.FlakySummary) {
	writeString(w, "flaky")
	if flaky.Identity.TestName != "" {
		writef(w, "\ttest=%s", flaky.Identity.TestName)
	}
	if flaky.Identity.CaseName != "" {
		writef(w, "\tcase=%s", flaky.Identity.CaseName)
	}
	if flaky.Identity.Metric != "" {
		writef(w, "\tmetric=%s", flaky.Identity.Metric)
	}
	writef(
		w,
		"\tcount=%d\tpassed=%d\tfailed=%d\tmean_score=%.3f\tstddev=%.3f\tmixed_pass=%t\tscore_flaky=%t\n",
		flaky.Count,
		flaky.Passed,
		flaky.Failed,
		flaky.MeanScore,
		flaky.StdDev,
		flaky.MixedPass,
		flaky.ScoreFlaky,
	)
}

func printEntry(w io.Writer, entry compare.Entry) {
	writef(w, "%s", entry.Status)
	if entry.Identity.TestName != "" {
		writef(w, "\t%s", entry.Identity.TestName)
	}
	if entry.Identity.CaseName != "" {
		writef(w, "\tcase=%s", entry.Identity.CaseName)
	}
	if entry.Identity.TestName == "" && entry.Identity.CaseName == "" {
		writef(w, "\t%s", entry.Identity.Metric)
	}
	writef(w, "\tmetric=%s", entry.Identity.Metric)

	switch {
	case entry.HasBaseline && entry.HasCurrent:
		writef(
			w,
			"\tscore_delta=%+.3f\tpass=%s\ttokens_delta=%+d\tlatency_delta_ns=%+d",
			entry.Delta.Score,
			entry.Delta.Pass,
			entry.Delta.Tokens,
			entry.Delta.LatencyNS,
		)
	case entry.HasCurrent:
		writef(w, "\tcurrent_score=%.3f\tcurrent_passed=%t", entry.Current.Score, entry.Current.Passed)
	case entry.HasBaseline:
		writef(w, "\tbaseline_score=%.3f\tbaseline_passed=%t", entry.Baseline.Score, entry.Baseline.Passed)
	}
	if entry.Decision.Failed {
		writef(w, "\tpolicy_failed=%s", entry.Decision.Reason)
	}
	writeln(w)

	for _, dimension := range entry.Dimensions {
		if dimension.Status == compare.StatusUnchanged {
			continue
		}
		printDimension(w, dimension)
	}
}

func printDimension(w io.Writer, dimension compare.DimensionEntry) {
	writef(w, "  dimension\t%s\t%s", dimension.Status, dimension.Name)
	switch {
	case dimension.HasBaseline && dimension.HasCurrent:
		writef(
			w,
			"\tscore_delta=%+.3f\tthreshold_delta=%+.3f\tpass=%s",
			dimension.Delta.Score,
			dimension.Delta.Threshold,
			dimension.Delta.Pass,
		)
	case dimension.HasCurrent:
		writef(w, "\tcurrent_score=%.3f\tcurrent_passed=%t", dimension.Current.Score, dimension.Current.Passed)
	case dimension.HasBaseline:
		writef(w, "\tbaseline_score=%.3f\tbaseline_passed=%t", dimension.Baseline.Score, dimension.Baseline.Passed)
	}
	writeln(w)
}

func setEnv(env []string, key string, value string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		next = append(next, entry)
	}
	return append(next, prefix+value)
}

func unsetEnv(env []string, key string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		next = append(next, entry)
	}
	return next
}

func lookupEnvValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

type evalCommandArgs struct {
	datasetPath string
	outPath     string
	metric      string
	pattern     string
	path        string
	minFields   int
}

type compareCommandArgs struct {
	baselinePath     string
	currentPath      string
	policyPath       string
	configPath       string
	caseIDKey        string
	scoreTolerance   *float64
	failOnRegression *bool
	format           string
}

type summarizeCommandArgs struct {
	resultPath string
	policyPath string
	configPath string
	caseIDKey  string
}

type reportCommandArgs struct {
	resultPath   string
	baselinePath string
	currentPath  string
	outPath      string
	format       string
}

type calibrateCommandArgs struct {
	resultPath     string
	judgeKey       string
	caseIDKey      string
	pairwiseKey    string
	scoreTolerance float64
	format         string
}

type testCommandArgs struct {
	profile    string
	configPath string
	store      bool
	runsDir    string
	runID      string
	runName    string
	forwarded  []string
}

func parseTestArgs(args []string) (testCommandArgs, error) {
	var parsed testCommandArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--profile":
			i++
			if i >= len(args) {
				return parsed, errors.New("--profile requires a value")
			}
			parsed.profile = args[i]
		case strings.HasPrefix(arg, "--profile="):
			parsed.profile = strings.TrimPrefix(arg, "--profile=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return parsed, errors.New("--config requires a value")
			}
			parsed.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			parsed.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--store":
			parsed.store = true
		case arg == "--runs-dir":
			i++
			if i >= len(args) {
				return parsed, errors.New("--runs-dir requires a value")
			}
			parsed.runsDir = args[i]
		case strings.HasPrefix(arg, "--runs-dir="):
			parsed.runsDir = strings.TrimPrefix(arg, "--runs-dir=")
		case arg == "--run-id":
			i++
			if i >= len(args) {
				return parsed, errors.New("--run-id requires a value")
			}
			parsed.runID = args[i]
		case strings.HasPrefix(arg, "--run-id="):
			parsed.runID = strings.TrimPrefix(arg, "--run-id=")
		case arg == "--run-name":
			i++
			if i >= len(args) {
				return parsed, errors.New("--run-name requires a value")
			}
			parsed.runName = args[i]
		case strings.HasPrefix(arg, "--run-name="):
			parsed.runName = strings.TrimPrefix(arg, "--run-name=")
		default:
			parsed.forwarded = append(parsed.forwarded, arg)
		}
	}
	return parsed, nil
}

func parseEvalArgs(args []string) (evalCommandArgs, error) {
	var parsed evalCommandArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dataset":
			i++
			if i >= len(args) {
				return parsed, errors.New("--dataset requires a value")
			}
			parsed.datasetPath = args[i]
		case strings.HasPrefix(arg, "--dataset="):
			parsed.datasetPath = strings.TrimPrefix(arg, "--dataset=")
		case arg == "--out":
			i++
			if i >= len(args) {
				return parsed, errors.New("--out requires a value")
			}
			parsed.outPath = args[i]
		case strings.HasPrefix(arg, "--out="):
			parsed.outPath = strings.TrimPrefix(arg, "--out=")
		case arg == "--metric":
			i++
			if i >= len(args) {
				return parsed, errors.New("--metric requires a value")
			}
			parsed.metric = args[i]
		case strings.HasPrefix(arg, "--metric="):
			parsed.metric = strings.TrimPrefix(arg, "--metric=")
		case arg == "--pattern":
			i++
			if i >= len(args) {
				return parsed, errors.New("--pattern requires a value")
			}
			parsed.pattern = args[i]
		case strings.HasPrefix(arg, "--pattern="):
			parsed.pattern = strings.TrimPrefix(arg, "--pattern=")
		case arg == "--path":
			i++
			if i >= len(args) {
				return parsed, errors.New("--path requires a value")
			}
			parsed.path = args[i]
		case strings.HasPrefix(arg, "--path="):
			parsed.path = strings.TrimPrefix(arg, "--path=")
		case arg == "--min-fields":
			i++
			if i >= len(args) {
				return parsed, errors.New("--min-fields requires a value")
			}
			value, err := strconv.Atoi(args[i])
			if err != nil {
				return parsed, fmt.Errorf("--min-fields: %w", err)
			}
			parsed.minFields = value
		case strings.HasPrefix(arg, "--min-fields="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--min-fields="))
			if err != nil {
				return parsed, fmt.Errorf("--min-fields: %w", err)
			}
			parsed.minFields = value
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown flag %q", arg)
		default:
			return parsed, fmt.Errorf("unexpected positional argument %q", arg)
		}
	}
	if parsed.datasetPath == "" {
		return parsed, errors.New("--dataset is required")
	}
	if parsed.metric == "" {
		return parsed, errors.New("--metric is required")
	}
	return parsed, nil
}

func parseSummarizeArgs(args []string) (summarizeCommandArgs, error) {
	var parsed summarizeCommandArgs
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--policy":
			i++
			if i >= len(args) {
				return parsed, errors.New("--policy requires a value")
			}
			parsed.policyPath = args[i]
		case strings.HasPrefix(arg, "--policy="):
			parsed.policyPath = strings.TrimPrefix(arg, "--policy=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return parsed, errors.New("--config requires a value")
			}
			parsed.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			parsed.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--case-id-key":
			i++
			if i >= len(args) {
				return parsed, errors.New("--case-id-key requires a value")
			}
			parsed.caseIDKey = args[i]
		case strings.HasPrefix(arg, "--case-id-key="):
			parsed.caseIDKey = strings.TrimPrefix(arg, "--case-id-key=")
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown flag %q", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) != 1 {
		return parsed, errors.New("usage: goeval summarize <results.jsonl>")
	}
	parsed.resultPath = positionals[0]
	return parsed, nil
}

func parseReportArgs(args []string) (reportCommandArgs, error) {
	var parsed reportCommandArgs
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--out":
			i++
			if i >= len(args) {
				return parsed, errors.New("--out requires a value")
			}
			parsed.outPath = args[i]
		case strings.HasPrefix(arg, "--out="):
			parsed.outPath = strings.TrimPrefix(arg, "--out=")
		case arg == "--format":
			i++
			if i >= len(args) {
				return parsed, errors.New("--format requires a value")
			}
			parsed.format = args[i]
		case strings.HasPrefix(arg, "--format="):
			parsed.format = strings.TrimPrefix(arg, "--format=")
		case arg == "--baseline":
			i++
			if i >= len(args) {
				return parsed, errors.New("--baseline requires a value")
			}
			parsed.baselinePath = args[i]
		case strings.HasPrefix(arg, "--baseline="):
			parsed.baselinePath = strings.TrimPrefix(arg, "--baseline=")
		case arg == "--current":
			i++
			if i >= len(args) {
				return parsed, errors.New("--current requires a value")
			}
			parsed.currentPath = args[i]
		case strings.HasPrefix(arg, "--current="):
			parsed.currentPath = strings.TrimPrefix(arg, "--current=")
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown flag %q", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if parsed.format != "" && parsed.format != "html" && parsed.format != "markdown" && parsed.format != "json" {
		return parsed, errors.New("--format must be html, markdown, or json")
	}
	if parsed.baselinePath != "" || parsed.currentPath != "" {
		if len(positionals) != 0 {
			return parsed, errors.New("usage: goeval report --baseline <baseline.jsonl> --current <current.jsonl>")
		}
		return parsed, nil
	}
	if len(positionals) != 1 {
		return parsed, errors.New("usage: goeval report <results.jsonl>")
	}
	parsed.resultPath = positionals[0]
	return parsed, nil
}

func parseCalibrateArgs(args []string) (calibrateCommandArgs, error) {
	parsed := calibrateCommandArgs{
		judgeKey:       "judge",
		scoreTolerance: 0.05,
		format:         "text",
	}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--judge-key":
			i++
			if i >= len(args) {
				return parsed, errors.New("--judge-key requires a value")
			}
			parsed.judgeKey = args[i]
		case strings.HasPrefix(arg, "--judge-key="):
			parsed.judgeKey = strings.TrimPrefix(arg, "--judge-key=")
		case arg == "--case-id-key":
			i++
			if i >= len(args) {
				return parsed, errors.New("--case-id-key requires a value")
			}
			parsed.caseIDKey = args[i]
		case strings.HasPrefix(arg, "--case-id-key="):
			parsed.caseIDKey = strings.TrimPrefix(arg, "--case-id-key=")
		case arg == "--pairwise-key":
			i++
			if i >= len(args) {
				return parsed, errors.New("--pairwise-key requires a value")
			}
			parsed.pairwiseKey = args[i]
		case strings.HasPrefix(arg, "--pairwise-key="):
			parsed.pairwiseKey = strings.TrimPrefix(arg, "--pairwise-key=")
		case arg == "--score-tolerance":
			i++
			if i >= len(args) {
				return parsed, errors.New("--score-tolerance requires a value")
			}
			value, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				return parsed, fmt.Errorf("--score-tolerance: %w", err)
			}
			parsed.scoreTolerance = value
		case strings.HasPrefix(arg, "--score-tolerance="):
			value, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--score-tolerance="), 64)
			if err != nil {
				return parsed, fmt.Errorf("--score-tolerance: %w", err)
			}
			parsed.scoreTolerance = value
		case arg == "--format":
			i++
			if i >= len(args) {
				return parsed, errors.New("--format requires a value")
			}
			parsed.format = args[i]
		case strings.HasPrefix(arg, "--format="):
			parsed.format = strings.TrimPrefix(arg, "--format=")
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown flag %q", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if parsed.format != "text" && parsed.format != "json" {
		return parsed, errors.New("--format must be text or json")
	}
	if len(positionals) != 1 {
		return parsed, errors.New("usage: goeval calibrate <results.jsonl>")
	}
	parsed.resultPath = positionals[0]
	return parsed, nil
}

func parseCompareArgs(args []string) (compareCommandArgs, error) {
	parsed := compareCommandArgs{format: "text"}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--policy":
			i++
			if i >= len(args) {
				return parsed, errors.New("--policy requires a value")
			}
			parsed.policyPath = args[i]
		case strings.HasPrefix(arg, "--policy="):
			parsed.policyPath = strings.TrimPrefix(arg, "--policy=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return parsed, errors.New("--config requires a value")
			}
			parsed.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			parsed.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--case-id-key":
			i++
			if i >= len(args) {
				return parsed, errors.New("--case-id-key requires a value")
			}
			parsed.caseIDKey = args[i]
		case strings.HasPrefix(arg, "--case-id-key="):
			parsed.caseIDKey = strings.TrimPrefix(arg, "--case-id-key=")
		case arg == "--score-tolerance":
			i++
			if i >= len(args) {
				return parsed, errors.New("--score-tolerance requires a value")
			}
			value, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				return parsed, fmt.Errorf("--score-tolerance: %w", err)
			}
			parsed.scoreTolerance = &value
		case strings.HasPrefix(arg, "--score-tolerance="):
			value, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--score-tolerance="), 64)
			if err != nil {
				return parsed, fmt.Errorf("--score-tolerance: %w", err)
			}
			parsed.scoreTolerance = &value
		case arg == "--fail-on-regression":
			i++
			if i >= len(args) {
				return parsed, errors.New("--fail-on-regression requires a value")
			}
			value, err := strconv.ParseBool(args[i])
			if err != nil {
				return parsed, fmt.Errorf("--fail-on-regression: %w", err)
			}
			parsed.failOnRegression = &value
		case strings.HasPrefix(arg, "--fail-on-regression="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--fail-on-regression="))
			if err != nil {
				return parsed, fmt.Errorf("--fail-on-regression: %w", err)
			}
			parsed.failOnRegression = &value
		case arg == "--format":
			i++
			if i >= len(args) {
				return parsed, errors.New("--format requires a value")
			}
			parsed.format = args[i]
		case strings.HasPrefix(arg, "--format="):
			parsed.format = strings.TrimPrefix(arg, "--format=")
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown flag %q", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if parsed.format != "text" && parsed.format != "json" {
		return parsed, fmt.Errorf("--format must be text or json")
	}
	if len(positionals) != 2 {
		return parsed, errors.New("usage: goeval compare <baseline.jsonl> <current.jsonl>")
	}
	parsed.baselinePath = positionals[0]
	parsed.currentPath = positionals[1]
	return parsed, nil
}

func loadSummaryPolicy(args summarizeCommandArgs) (compare.Policy, bool, error) {
	var policy compare.Policy
	usePolicy := false
	if args.configPath != "" {
		manifest, _, err := loadManifest(args.configPath, true)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read config: %w", err)
		}
		policy = manifest.Compare
		usePolicy = true
	} else {
		manifest, ok, err := loadManifest(defaultConfigPath, false)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read config: %w", err)
		}
		if ok {
			policy = manifest.Compare
			usePolicy = true
		}
	}
	if args.policyPath != "" {
		loaded, err := loadPolicyFile(args.policyPath)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read policy: %w", err)
		}
		policy = loaded
		usePolicy = true
	}
	if args.caseIDKey != "" {
		policy.CaseIDKey = args.caseIDKey
		usePolicy = true
	}
	return policy, usePolicy, nil
}

func loadComparePolicy(args compareCommandArgs) (compare.Policy, bool, error) {
	var policy compare.Policy
	usePolicy := false
	if args.configPath != "" {
		manifest, _, err := loadManifest(args.configPath, true)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read config: %w", err)
		}
		policy = manifest.Compare
		usePolicy = true
	} else {
		manifest, ok, err := loadManifest(defaultConfigPath, false)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read config: %w", err)
		}
		if ok {
			policy = manifest.Compare
			usePolicy = true
		}
	}
	if args.policyPath != "" {
		loaded, err := loadPolicyFile(args.policyPath)
		if err != nil {
			return compare.Policy{}, false, fmt.Errorf("read policy: %w", err)
		}
		policy = loaded
		usePolicy = true
	}
	if args.caseIDKey != "" {
		policy.CaseIDKey = args.caseIDKey
		usePolicy = true
	}
	if args.scoreTolerance != nil {
		policy.Default.ScoreTolerance = args.scoreTolerance
		usePolicy = true
	}
	if args.failOnRegression != nil {
		policy.Default.FailOnRegression = args.failOnRegression
		usePolicy = true
	}
	return policy, usePolicy, nil
}

func printMissingPrerequisites(w io.Writer, profile string, action string, missing []missingPrerequisite) {
	writef(w, "goeval profile %q %s: missing prerequisites\n", profile, action)
	for _, item := range missing {
		writef(w, "  - %s: %s\n", item.Name, item.Reason)
	}
}

func writeString(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeln(w io.Writer) {
	_, _ = fmt.Fprintln(w)
}
