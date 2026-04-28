package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
)

var version = "dev"

type goCommandFunc func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) int

const usage = `Usage:
  goeval test [go test args...]
  goeval compare <baseline.jsonl> <current.jsonl>
  goeval version

Commands:
  test     Run go test with GOEVAL=1 set.
  compare  Compare two go-eval JSONL result files.
  version  Print the goeval CLI version.
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
		if goCmd == nil {
			goCmd = runGoCommand
		}
		goArgs := append([]string{"test"}, args[1:]...)
		env := setEnv(baseEnv, eval.EnvVar, "1")
		return goCmd(ctx, goArgs, env, stdin, stdout, stderr)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "version":
		return runVersion(stdout)
	default:
		writef(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
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
	if len(args) != 2 {
		writef(stderr, "usage: goeval compare <baseline.jsonl> <current.jsonl>\n")
		return 2
	}

	report, err := compare.CompareFiles(args[0], args[1])
	if err != nil {
		writef(stderr, "compare: %v\n", err)
		return 1
	}

	printCompareReport(stdout, report)
	if report.Summary.Regressed > 0 || report.Summary.Missing > 0 {
		return 1
	}
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
		"Summary: total=%d added=%d missing=%d improved=%d regressed=%d unchanged=%d\n",
		s.Total,
		s.Added,
		s.Missing,
		s.Improved,
		s.Regressed,
		s.Unchanged,
	)

	for _, entry := range report.Entries {
		if entry.Status == compare.StatusUnchanged {
			continue
		}
		printEntry(w, entry)
	}
}

func printEntry(w io.Writer, entry compare.Entry) {
	writef(w, "%s\t%s", entry.Status, entry.Identity.TestName)
	if entry.Identity.CaseName != "" {
		writef(w, "\tcase=%s", entry.Identity.CaseName)
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

func writeString(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeln(w io.Writer) {
	_, _ = fmt.Fprintln(w)
}
