package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
	"github.com/igcodinap/go-eval/internal/runstore"
)

const (
	runStatusPassed = "passed"
	runStatusFailed = "failed"
	runStatusError  = "error"
)

type storedTestRequest struct {
	profile  string
	packages []string
	runsDir  string
	runID    string
	runName  string
}

type testEvent struct {
	Time    string  `json:"Time,omitempty"`
	Action  string  `json:"Action"`
	Package string  `json:"Package,omitempty"`
	Test    string  `json:"Test,omitempty"`
	Elapsed float64 `json:"Elapsed,omitempty"`
	Output  string  `json:"Output,omitempty"`
}

type testEventStats struct {
	Events          int
	MalformedLines  int
	TestFailures    int
	PackageFailures int
}

func runGoTestStored(
	ctx context.Context,
	goArgs []string,
	env []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	goCmd goCommandFunc,
	req storedTestRequest,
) int {
	start := time.Now()
	root, err := resolveRunsRoot(req.runsDir)
	if err != nil {
		writef(stderr, "test: resolve runs dir: %v\n", err)
		return 1
	}
	store := runstore.New(root)
	repo, branch, commit := gitMetadata()

	runID, runDir, err := reserveStoredRun(store, start, branch, commit, req.runID)
	if err != nil {
		writef(stderr, "test: create run dir: %v\n", err)
		return 1
	}

	goArgs = ensureGoTestJSON(goArgs)
	env = setEnv(env, eval.EnvVar, "1")
	env = setEnv(env, eval.ResultsDirEnvVar, runDir)

	eventPath := filepath.Join(runDir, "test-events.jsonl")
	eventFile, err := os.OpenFile(eventPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		writef(stderr, "test: create test events: %v\n", err)
		_ = writeStoredManifest(stderr, store, runID, req, goArgs, runDir, repo, branch, commit, start, time.Now(), 1, runStatusError, []string{err.Error()})
		return 1
	}

	eventWriter := newTestEventWriter(eventFile, stdout)
	code := goCmd(ctx, goArgs, env, stdin, eventWriter, stderr)
	flushErr := eventWriter.Flush()
	closeErr := eventFile.Close()

	end := time.Now()
	diagnostics := eventWriter.Diagnostics()
	if flushErr != nil {
		diagnostics = append(diagnostics, "flush test events: "+flushErr.Error())
	}
	if closeErr != nil {
		diagnostics = append(diagnostics, "close test events: "+closeErr.Error())
	}

	resultsPath := filepath.Join(runDir, "results.jsonl")
	summaryPath := filepath.Join(runDir, "summary.json")
	reportPath := filepath.Join(runDir, "report.html")
	summary, haveSummary, artifactDiagnostics := writeStoredArtifacts(resultsPath, summaryPath, reportPath)
	diagnostics = append(diagnostics, artifactDiagnostics...)

	status := storedRunStatus(code, eventWriter.Stats(), len(artifactDiagnostics) > 0)
	manifestErr := writeStoredManifest(stderr, store, runID, req, goArgs, runDir, repo, branch, commit, start, end, code, status, diagnostics)
	if manifestErr != nil {
		if code != 0 {
			return code
		}
		return 1
	}

	indexErr := updateStoredRunIndex(store, runID, runDir, req.profile, branch, commit, status, start, end, summary, haveSummary)
	if indexErr != nil {
		writef(stderr, "test: update run index: %v\n", indexErr)
		if code == 0 {
			return 1
		}
	}
	if latestErr := store.WriteLatest(runID); latestErr != nil {
		writef(stderr, "test: update latest run: %v\n", latestErr)
		if code == 0 {
			return 1
		}
	}

	if code != 0 {
		return code
	}
	if len(artifactDiagnostics) > 0 || flushErr != nil || closeErr != nil {
		return 1
	}
	return 0
}

func reserveStoredRun(store runstore.Store, start time.Time, branch string, commit string, customID string) (string, string, error) {
	if customID != "" {
		id, err := store.ValidateCustomRunID(customID)
		if err != nil {
			return "", "", err
		}
		dir, err := store.EnsureRunDir(id)
		return id, dir, err
	}
	for attempt := 0; attempt < 100; attempt++ {
		id, err := store.NewRunID(start, branch, commit)
		if err != nil {
			return "", "", err
		}
		dir, err := store.EnsureRunDir(id)
		if err == nil {
			return id, dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
	}
	return "", "", errors.New("could not reserve a unique run id")
}

func resolveRunsRoot(path string) (string, error) {
	if path == "" {
		return runstore.DefaultRoot(""), nil
	}
	return filepath.Abs(path)
}

func ensureGoTestJSON(args []string) []string {
	if len(args) == 0 {
		return []string{"test", "-json"}
	}
	next := append([]string(nil), args...)
	for i := 1; i < len(next); i++ {
		if next[i] == "-args" {
			break
		}
		switch next[i] {
		case "-json", "-json=true":
			return next
		case "-json=false":
			next[i] = "-json"
			return next
		}
		if strings.HasPrefix(next[i], "-json=") {
			next[i] = "-json"
			return next
		}
	}
	return append([]string{next[0], "-json"}, next[1:]...)
}

func storedRunStatus(code int, stats testEventStats, artifactFailure bool) string {
	if code == 0 {
		if artifactFailure {
			return runStatusError
		}
		return runStatusPassed
	}
	if stats.TestFailures > 0 {
		return runStatusFailed
	}
	return runStatusError
}

func writeStoredManifest(
	stderr io.Writer,
	store runstore.Store,
	runID string,
	req storedTestRequest,
	goArgs []string,
	runDir string,
	repo string,
	branch string,
	commit string,
	start time.Time,
	end time.Time,
	code int,
	status string,
	diagnostics []string,
) error {
	manifest := eval.NewRunManifest()
	manifest.GoEvalVersion = version
	manifest.RunID = runID
	manifest.RunName = req.runName
	manifest.Repo = repo
	manifest.Branch = branch
	manifest.Commit = commit
	manifest.ExitCode = code
	manifest.Status = status
	manifest.Command = append([]string{"go"}, goArgs...)
	manifest.Profile = req.profile
	manifest.Packages = append([]string(nil), req.packages...)
	manifest.ResultsPath = filepath.Join(runDir, "results.jsonl")
	manifest.TracesPath = filepath.Join(runDir, "traces.jsonl")
	manifest.JudgeEventsPath = filepath.Join(runDir, "judge-events.jsonl")
	manifest.TestEventsPath = filepath.Join(runDir, "test-events.jsonl")
	manifest.SummaryPath = filepath.Join(runDir, "summary.json")
	manifest.ReportPath = filepath.Join(runDir, "report.html")
	manifest.StartedAt = start.UTC().Format(time.RFC3339Nano)
	manifest.EndedAt = end.UTC().Format(time.RFC3339Nano)
	manifest.DurationNS = int64(end.Sub(start))
	if len(diagnostics) > 0 {
		manifest.Metadata = map[string]any{"diagnostics": diagnostics}
	}
	if err := eval.WriteRunManifest(store.ManifestPath(runID), manifest); err != nil {
		writef(stderr, "test: write run manifest: %v\n", err)
		return err
	}
	return nil
}

func writeStoredArtifacts(resultsPath string, summaryPath string, reportPath string) (compare.ResultsSummary, bool, []string) {
	var diagnostics []string
	if !fileExists(resultsPath) {
		return compare.ResultsSummary{}, false, nil
	}
	summary, err := compare.SummarizeFile(resultsPath)
	if err != nil {
		return compare.ResultsSummary{}, false, []string{"summarize results: " + err.Error()}
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		diagnostics = append(diagnostics, "encode summary: "+err.Error())
	} else {
		data = append(data, '\n')
		if err := runstore.WriteFileAtomic(summaryPath, data, 0o644); err != nil {
			diagnostics = append(diagnostics, "write summary: "+err.Error())
		}
	}
	report, err := compare.ReportHTML(compare.NewResultsReport(resultsPath, summary))
	if err != nil {
		diagnostics = append(diagnostics, "render report: "+err.Error())
	} else if err := runstore.WriteFileAtomic(reportPath, report, 0o644); err != nil {
		diagnostics = append(diagnostics, "write report: "+err.Error())
	}
	return summary, len(diagnostics) == 0, diagnostics
}

func updateStoredRunIndex(
	store runstore.Store,
	runID string,
	runDir string,
	profile string,
	branch string,
	commit string,
	status string,
	start time.Time,
	end time.Time,
	summary compare.ResultsSummary,
	haveSummary bool,
) error {
	records, err := store.Records()
	if err != nil {
		records = nil
	}
	next := records[:0]
	for _, record := range records {
		if record.ID != runID {
			next = append(next, record)
		}
	}
	record := runstore.RunRecord{
		ID:         runID,
		Path:       runDir,
		StartedAt:  start.UTC().Format(time.RFC3339Nano),
		EndedAt:    end.UTC().Format(time.RFC3339Nano),
		Branch:     branch,
		Commit:     commit,
		Profile:    profile,
		Status:     status,
		DurationNS: int64(end.Sub(start)),
	}
	if haveSummary {
		record.PassRate = summary.PassRate
		record.Failed = summary.Failed
	}
	next = append(next, record)
	return store.WriteIndex(next)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type testEventWriter struct {
	raw         io.Writer
	human       io.Writer
	buf         bytes.Buffer
	stats       testEventStats
	diagnostics []string
}

func newTestEventWriter(raw io.Writer, human io.Writer) *testEventWriter {
	return &testEventWriter{raw: raw, human: human}
}

func (w *testEventWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			w.processLine(w.buf.Bytes())
			w.buf.Reset()
			continue
		}
		_ = w.buf.WriteByte(b)
	}
	return len(p), nil
}

func (w *testEventWriter) Flush() error {
	if w.buf.Len() > 0 {
		w.processLine(w.buf.Bytes())
		w.buf.Reset()
	}
	return nil
}

func (w *testEventWriter) Stats() testEventStats {
	return w.stats
}

func (w *testEventWriter) Diagnostics() []string {
	return append([]string(nil), w.diagnostics...)
}

func (w *testEventWriter) processLine(line []byte) {
	if len(line) == 0 {
		return
	}
	if w.raw != nil {
		if _, err := w.raw.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			w.diagnostics = append(w.diagnostics, "write test event: "+err.Error())
		}
	}
	var event testEvent
	if err := json.Unmarshal(line, &event); err != nil {
		w.stats.MalformedLines++
		w.diagnostics = append(w.diagnostics, "malformed test event: "+err.Error())
		if w.human != nil {
			_, _ = w.human.Write(append(append([]byte(nil), line...), '\n'))
		}
		return
	}
	w.stats.Events++
	if event.Action == "fail" {
		if event.Test != "" {
			w.stats.TestFailures++
		} else {
			w.stats.PackageFailures++
		}
	}
	if event.Output != "" && w.human != nil {
		_, _ = io.WriteString(w.human, event.Output)
	}
}

func gitMetadata() (repo string, branch string, commit string) {
	repo = gitOutput("config", "--get", "remote.origin.url")
	branch = gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" || branch == "HEAD" {
		branch = "detached"
	}
	commit = gitOutput("rev-parse", "HEAD")
	if commit == "" {
		commit = "nogit"
	}
	return repo, branch, commit
}

func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
