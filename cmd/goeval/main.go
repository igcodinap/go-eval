package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
)

var version = "dev"

type goCommandFunc func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) int

const usage = `Usage:
  goeval test [--profile <name>] [--config <path>] [go test args...]
  goeval compare [--policy <path>] [--config <path>] [--case-id-key <key>] [--score-tolerance <float>] [--fail-on-regression=<bool>] [--format text|json] <baseline.jsonl> <current.jsonl>
  goeval summarize [--policy <path>] [--config <path>] [--case-id-key <key>] <results.jsonl>
  goeval version

Commands:
  test       Run go test with GOEVAL=1 set.
  compare    Compare two go-eval JSONL result files.
  summarize  Summarize one go-eval JSONL result file.
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
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "summarize":
		return runSummarize(args[1:], stdout, stderr)
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

	profileName, configPath, forwardedArgs, err := parseTestArgs(args)
	if err != nil {
		writef(stderr, "test: %v\n", err)
		return 2
	}
	if profileName == "" {
		if configPath != "" {
			writef(stderr, "test: --config requires --profile\n")
			return 2
		}
		goArgs := append([]string{"test"}, forwardedArgs...)
		env := setEnv(baseEnv, eval.EnvVar, "1")
		return goCmd(ctx, goArgs, env, stdin, stdout, stderr)
	}

	manifest, _, err := loadManifest(configPath, true)
	if err != nil {
		writef(stderr, "test: read config: %v\n", err)
		return 2
	}
	profile, ok := manifest.Profiles[profileName]
	if !ok {
		writef(stderr, "test: profile %q not found\n", profileName)
		return 2
	}

	missing := checkManifestPrerequisites(ctx, profile.Prerequisites)
	if len(missing) > 0 {
		if profile.MissingPrerequisite == "fail" {
			printMissingPrerequisites(stderr, profileName, "failed", missing)
			return 1
		}
		printMissingPrerequisites(stdout, profileName, "skipped", missing)
		return 0
	}

	testArgs := append([]string{"test"}, profile.Packages...)
	testArgs = append(testArgs, forwardedArgs...)
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
	return goCmd(ctx, testArgs, env, stdin, stdout, stderr)
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

func parseTestArgs(args []string) (string, string, []string, error) {
	var profile string
	var config string
	var forwarded []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--profile":
			i++
			if i >= len(args) {
				return "", "", nil, errors.New("--profile requires a value")
			}
			profile = args[i]
		case strings.HasPrefix(arg, "--profile="):
			profile = strings.TrimPrefix(arg, "--profile=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return "", "", nil, errors.New("--config requires a value")
			}
			config = args[i]
		case strings.HasPrefix(arg, "--config="):
			config = strings.TrimPrefix(arg, "--config=")
		default:
			forwarded = append(forwarded, arg)
		}
	}
	return profile, config, forwarded, nil
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
