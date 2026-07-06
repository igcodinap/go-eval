package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	eval "github.com/igcodinap/go-eval"
	"github.com/igcodinap/go-eval/compare"
)

type recordingGoCommand struct {
	code  int
	calls int
	args  []string
	env   []string
}

func (r *recordingGoCommand) run(_ context.Context, args []string, env []string, _ io.Reader, _ io.Writer, _ io.Writer) int {
	r.calls++
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), env...)
	return r.code
}

type writingGoCommand struct {
	code int
	args []string
	env  []string
	fn   func([]string, io.Writer) error
}

func (r *writingGoCommand) run(_ context.Context, args []string, env []string, _ io.Reader, stdout io.Writer, _ io.Writer) int {
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), env...)
	if r.fn != nil {
		if err := r.fn(env, stdout); err != nil {
			return 1
		}
	}
	return r.code
}

func TestRunTestSetsGOEVALAndForwardsArgs(t *testing.T) {
	recorder := &recordingGoCommand{code: 17}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(
		context.Background(),
		[]string{"test", "./...", "-run", "TestEval", "-count=1"},
		[]string{"PATH=/bin", "GOEVAL=0", "OTHER=value"},
		nil,
		&stdout,
		&stderr,
		recorder.run,
	)

	if code != 17 {
		t.Fatalf("exit code = %d, want 17", code)
	}
	if recorder.calls != 1 {
		t.Fatalf("go command calls = %d, want 1", recorder.calls)
	}
	wantArgs := []string{"test", "./...", "-run", "TestEval", "-count=1"}
	if !reflect.DeepEqual(recorder.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", recorder.args, wantArgs)
	}
	value, count := envValue(recorder.env, "GOEVAL")
	if value != "1" || count != 1 {
		t.Fatalf("GOEVAL = %q count=%d, want value 1 count 1 in %#v", value, count, recorder.env)
	}
	if !containsEnv(recorder.env, "OTHER=value") {
		t.Fatalf("expected unrelated env to be preserved in %#v", recorder.env)
	}
}

func TestRunTestProfileLoadsManifestAndForwardsEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "goeval.json")
	resultsDir := filepath.Join(dir, "results")
	if err := os.WriteFile(configPath, []byte(`{
		"profiles": {
			"pr": {
				"packages": ["./..."],
				"tiers": ["critical", "standard"],
				"results_dir": "`+filepath.ToSlash(resultsDir)+`"
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(
		context.Background(),
		[]string{"test", "--profile", "pr", "--config", configPath, "-run", "TestEval"},
		[]string{"PATH=/bin", "GOEVAL=0", "GOEVAL_TIER=old", "GOEVAL_RESULTS_DIR=old"},
		nil,
		&stdout,
		&stderr,
		recorder.run,
	)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	wantArgs := []string{"test", "./...", "-run", "TestEval"}
	if !reflect.DeepEqual(recorder.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", recorder.args, wantArgs)
	}
	if got, _ := envValue(recorder.env, "GOEVAL_TIER"); got != "critical,standard" {
		t.Fatalf("GOEVAL_TIER = %q", got)
	}
	if got, _ := envValue(recorder.env, "GOEVAL_RESULTS_DIR"); got != resultsDir {
		t.Fatalf("GOEVAL_RESULTS_DIR = %q, want %q", got, resultsDir)
	}

	manifestPath := filepath.Join(resultsDir, "goeval-run.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	var manifest struct {
		SchemaVersion        int      `json:"schema_version"`
		GoEvalVersion        string   `json:"goeval_version"`
		ResultsSchemaVersion int      `json:"results_schema_version"`
		TraceSchemaVersion   int      `json:"trace_schema_version"`
		Command              []string `json:"command"`
		Profile              string   `json:"profile"`
		ResultsPath          string   `json:"results_path"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Unmarshal manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.ResultsSchemaVersion != 1 || manifest.TraceSchemaVersion != 1 {
		t.Fatalf("unexpected schema versions: %+v", manifest)
	}
	if manifest.GoEvalVersion == "" || manifest.Profile != "pr" || manifest.ResultsPath != filepath.Join(resultsDir, "results.jsonl") {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestRunTestStoreCreatesRunAndRendersOutput(t *testing.T) {
	runsDir := t.TempDir()
	recorder := &writingGoCommand{
		code: 0,
		fn: func(env []string, stdout io.Writer) error {
			resultsDir, _ := envValue(env, "GOEVAL_RESULTS_DIR")
			if err := os.WriteFile(filepath.Join(resultsDir, "results.jsonl"), []byte(
				`{"test_name":"TestEval/pass","metric":"Contains","score":1,"passed":true,"tokens":3,"latency_ns":10}`+"\n",
			), 0o644); err != nil {
				return err
			}
			_, _ = io.WriteString(stdout, `{"Action":"run","Package":"pkg","Test":"TestEval/pass"}`+"\n")
			_, _ = io.WriteString(stdout, `{"Action":"output","Package":"pkg","Test":"TestEval/pass","Output":"=== RUN   TestEval/pass\n"}`+"\n")
			_, _ = io.WriteString(stdout, `{"Action":"pass","Package":"pkg","Test":"TestEval/pass","Elapsed":0.01}`+"\n")
			return nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(
		context.Background(),
		[]string{"test", "--store", "--runs-dir", runsDir, "--run-id", "smoke", "./..."},
		[]string{"PATH=/bin", "GOEVAL_RESULTS_DIR=old"},
		nil,
		&stdout,
		&stderr,
		recorder.run,
	)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !reflect.DeepEqual(recorder.args, []string{"test", "-json", "./..."}) {
		t.Fatalf("args = %#v, want go test -json ./...", recorder.args)
	}
	resultsDir, count := envValue(recorder.env, "GOEVAL_RESULTS_DIR")
	if count != 1 || resultsDir != filepath.Join(runsDir, "runs", "smoke") {
		t.Fatalf("GOEVAL_RESULTS_DIR = %q count=%d", resultsDir, count)
	}
	if !strings.Contains(stdout.String(), "=== RUN   TestEval/pass") || strings.Contains(stdout.String(), `"Action"`) {
		t.Fatalf("stdout should be human output, got:\n%s", stdout.String())
	}
	runDir := filepath.Join(runsDir, "runs", "smoke")
	for _, name := range []string{"goeval-run.json", "results.jsonl", "test-events.jsonl", "summary.json", "report.html"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	events, err := os.ReadFile(filepath.Join(runDir, "test-events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile test events: %v", err)
	}
	if !strings.Contains(string(events), `"Action":"output"`) {
		t.Fatalf("test-events.jsonl missing raw events:\n%s", events)
	}
	manifest, err := eval.ReadRunManifest(filepath.Join(runDir, "goeval-run.json"))
	if err != nil {
		t.Fatalf("ReadRunManifest: %v", err)
	}
	if manifest.RunID != "smoke" || manifest.Status != "passed" || manifest.TestEventsPath == "" || manifest.SummaryPath == "" || manifest.ReportPath == "" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	latest, err := os.ReadFile(filepath.Join(runsDir, "latest"))
	if err != nil {
		t.Fatalf("ReadFile latest: %v", err)
	}
	if strings.TrimSpace(string(latest)) != "smoke" {
		t.Fatalf("latest = %q, want smoke", latest)
	}
}

func TestRunTestStorePreservesFailureExitAndStatus(t *testing.T) {
	runsDir := t.TempDir()
	recorder := &writingGoCommand{
		code: 1,
		fn: func(_ []string, stdout io.Writer) error {
			_, _ = io.WriteString(stdout, `{"Action":"run","Package":"pkg","Test":"TestEval/fail"}`+"\n")
			_, _ = io.WriteString(stdout, `{"Action":"output","Package":"pkg","Test":"TestEval/fail","Output":"failure details\n"}`+"\n")
			_, _ = io.WriteString(stdout, `{"Action":"fail","Package":"pkg","Test":"TestEval/fail","Elapsed":0.01}`+"\n")
			return nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--store", "--runs-dir", runsDir, "--run-id", "failed"}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 1 {
		t.Fatalf("exit code = %d, want go test failure 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "failure details") {
		t.Fatalf("stdout missing rendered failure output:\n%s", stdout.String())
	}
	manifest, err := eval.ReadRunManifest(filepath.Join(runsDir, "runs", "failed", "goeval-run.json"))
	if err != nil {
		t.Fatalf("ReadRunManifest: %v", err)
	}
	if manifest.Status != "failed" || manifest.ExitCode != 1 {
		t.Fatalf("manifest status = %q exit=%d, want failed exit 1", manifest.Status, manifest.ExitCode)
	}
}

func TestRunTestStoreManifestFailureDoesNotUpdateAliases(t *testing.T) {
	runsDir := t.TempDir()
	recorder := &writingGoCommand{
		code: 1,
		fn: func(env []string, stdout io.Writer) error {
			resultsDir, _ := envValue(env, "GOEVAL_RESULTS_DIR")
			if err := os.Mkdir(filepath.Join(resultsDir, "goeval-run.json"), 0o755); err != nil {
				return err
			}
			_, _ = io.WriteString(stdout, `{"Action":"run","Package":"pkg","Test":"TestEval/fail"}`+"\n")
			_, _ = io.WriteString(stdout, `{"Action":"fail","Package":"pkg","Test":"TestEval/fail","Elapsed":0.01}`+"\n")
			return nil
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--store", "--runs-dir", runsDir, "--run-id", "manifest-fail"}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 1 {
		t.Fatalf("exit code = %d, want original go test failure 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "test: write run manifest:") {
		t.Fatalf("stderr missing manifest failure:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(runsDir, "latest")); !os.IsNotExist(err) {
		t.Fatalf("latest should not be updated after manifest failure, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(runsDir, "index.json")); !os.IsNotExist(err) {
		t.Fatalf("index should not be updated after manifest failure, stat err=%v", err)
	}
}

func TestRunTestWithoutStoreDoesNotCreateRunStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/plain\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "./..."}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".goeval")); !os.IsNotExist(err) {
		t.Fatalf("plain goeval test should not create .goeval, stat err=%v", err)
	}
}

func TestRunTestStoreFlagsRequireStore(t *testing.T) {
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--run-id", "ignored", "./..."}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 2 {
		t.Fatalf("exit code = %d, want usage error 2", code)
	}
	if recorder.calls != 0 {
		t.Fatalf("go command should not run when store-only flags are used without --store")
	}
	if !strings.Contains(stderr.String(), "require --store") {
		t.Fatalf("stderr missing store-only flag error:\n%s", stderr.String())
	}
}

func TestResolveRunsRootReturnsAbsoluteCustomPath(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	relativePath := filepath.Join("..", filepath.Base(dir), "custom-runs")
	got, err := resolveRunsRoot(relativePath)
	if err != nil {
		t.Fatalf("resolveRunsRoot: %v", err)
	}
	want, err := filepath.Abs(relativePath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if got != want {
		t.Fatalf("runs root = %q, want %q", got, want)
	}
}

func TestEnsureGoTestJSONStopsAtArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "adds before packages",
			args: []string{"test", "./...", "-run", "TestEval"},
			want: []string{"test", "-json", "./...", "-run", "TestEval"},
		},
		{
			name: "preserves go json flag",
			args: []string{"test", "-json", "./..."},
			want: []string{"test", "-json", "./..."},
		},
		{
			name: "replaces disabled go json flag",
			args: []string{"test", "./...", "-json=false"},
			want: []string{"test", "./...", "-json"},
		},
		{
			name: "ignores test binary json after args",
			args: []string{"test", "./pkg", "-args", "-json"},
			want: []string{"test", "-json", "./pkg", "-args", "-json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureGoTestJSON(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ensureGoTestJSON(%#v) = %#v, want %#v", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunEvalContainsWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	datasetPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(datasetPath, []byte(`{
		"cases": [
			{"name":"pass","output":"Paris is the capital of France","expected":"Paris","metadata":{"case_id":"fr"}}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile dataset: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"eval", "--metric", "contains", "--dataset", datasetPath}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"test_name":"pass"`) ||
		!strings.Contains(stdout.String(), `"metric":"Contains"`) ||
		!strings.Contains(stdout.String(), `"passed":true`) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRunEvalContainsReturnsFailureOnFailedCase(t *testing.T) {
	dir := t.TempDir()
	datasetPath := filepath.Join(dir, "cases.json")
	outPath := filepath.Join(dir, "results.jsonl")
	if err := os.WriteFile(datasetPath, []byte(`{
		"cases": [
			{"name":"fail","output":"Lyon","expected":"Paris"}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile dataset: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"eval", "--metric", "contains", "--dataset", datasetPath, "--out", outPath}, nil, nil, &stdout, &stderr, nil)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote "+outPath) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile results: %v", err)
	}
	if !strings.Contains(string(data), `"passed":false`) {
		t.Fatalf("expected failed result JSONL, got %s", data)
	}
}

func TestRunTestProfileDefaultsToAllPackages(t *testing.T) {
	configPath := writeConfigFile(t, `{
		"profiles": {
			"pr": {}
		}
	}`)
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--profile", "pr", "--config", configPath}, []string{"GOEVAL_TIER=old", "GOEVAL_RESULTS_DIR=old"}, nil, &stdout, &stderr, recorder.run)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	wantArgs := []string{"test", "./..."}
	if !reflect.DeepEqual(recorder.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", recorder.args, wantArgs)
	}
	if _, count := envValue(recorder.env, "GOEVAL_TIER"); count != 0 {
		t.Fatalf("GOEVAL_TIER should be unset in %#v", recorder.env)
	}
	if _, count := envValue(recorder.env, "GOEVAL_RESULTS_DIR"); count != 0 {
		t.Fatalf("GOEVAL_RESULTS_DIR should be unset in %#v", recorder.env)
	}
}

func TestRunTestProfileReportsUnknownProfile(t *testing.T) {
	configPath := writeConfigFile(t, `{
		"profiles": {
			"pr": {}
		}
	}`)
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--profile", "nightly", "--config", configPath}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if recorder.calls != 0 {
		t.Fatalf("go command should not run for unknown profile")
	}
	if !strings.Contains(stderr.String(), `profile "nightly" not found`) {
		t.Fatalf("stderr missing unknown profile message:\n%s", stderr.String())
	}
}

func TestRunTestProfileSkipsWhenPrerequisiteMissing(t *testing.T) {
	t.Setenv("GOEVAL_TEST_MISSING_KEY", "")
	configPath := writeConfigFile(t, `{
		"profiles": {
			"google": {
				"packages": ["./..."],
				"prerequisites": [{"type": "env", "name": "GOEVAL_TEST_MISSING_KEY"}],
				"missing_prerequisite": "skip"
			}
		}
	}`)
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--profile", "google", "--config", configPath}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if recorder.calls != 0 {
		t.Fatalf("go command should not run when prerequisite is missing")
	}
	if !strings.Contains(stdout.String(), `goeval profile "google" skipped: missing prerequisites`) ||
		!strings.Contains(stdout.String(), "env GOEVAL_TEST_MISSING_KEY") {
		t.Fatalf("stdout missing prerequisite summary:\n%s", stdout.String())
	}
}

func TestRunTestProfileFailsWhenPrerequisiteMissingAndConfigured(t *testing.T) {
	t.Setenv("GOEVAL_TEST_MISSING_KEY", "")
	configPath := writeConfigFile(t, `{
		"profiles": {
			"release": {
				"packages": ["./..."],
				"prerequisites": [{"type": "env", "name": "GOEVAL_TEST_MISSING_KEY"}],
				"missing_prerequisite": "fail"
			}
		}
	}`)
	recorder := &recordingGoCommand{code: 0}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"test", "--profile", "release", "--config", configPath}, nil, nil, &stdout, &stderr, recorder.run)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if recorder.calls != 0 {
		t.Fatalf("go command should not run when prerequisite is missing")
	}
	if !strings.Contains(stderr.String(), `goeval profile "release" failed: missing prerequisites`) {
		t.Fatalf("stderr missing prerequisite failure:\n%s", stderr.String())
	}
}

func TestLoadManifestRejectsInvalidPrerequisite(t *testing.T) {
	configPath := writeConfigFile(t, `{
		"profiles": {
			"bad": {
				"prerequisites": [{"type": "wat", "name": "NOPE"}]
			}
		}
	}`)

	_, _, err := loadManifest(configPath, true)
	if err == nil || !strings.Contains(err.Error(), `invalid prerequisite type "wat"`) {
		t.Fatalf("expected invalid prerequisite error, got %v", err)
	}
}

func TestLoadManifestRejectsMalformedPolicy(t *testing.T) {
	configPath := writeConfigFile(t, `{
		"compare": {
			"default": {"score_tolerance": -0.1}
		}
	}`)

	_, _, err := loadManifest(configPath, true)
	if err == nil || !strings.Contains(err.Error(), "score_tolerance must be non-negative") {
		t.Fatalf("expected malformed policy error, got %v", err)
	}
}

func TestManifestPrerequisitesSupportFileAndTCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()

	missing := checkManifestPrerequisites(context.Background(), []manifestPrerequisite{
		{Type: "file", Path: path},
		{Type: "tcp", Name: "local", Address: listener.Addr().String()},
	})

	if len(missing) != 0 {
		t.Fatalf("expected prerequisites to pass, got %+v", missing)
	}
}

func TestRunCompareReportsRegressionAndFails(t *testing.T) {
	baselinePath, currentPath := writeCompareFiles(t,
		`{"test_name":"TestEval/regress","metric":"Faithfulness","score":0.9,"passed":true,"tokens":10,"latency_ns":100}`+"\n",
		`{"test_name":"TestEval/regress","metric":"Faithfulness","score":0.7,"passed":true,"tokens":12,"latency_ns":150}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"compare", baselinePath, currentPath}, nil, nil, &stdout, &stderr, nil)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Summary: total=1 added=0 missing=0 improved=0 regressed=1 unchanged=0",
		"regressed\tTestEval/regress\tmetric=Faithfulness",
		"score_delta=-0.200",
		"tokens_delta=+2",
		"latency_delta_ns=+50",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, out)
		}
	}
}

func TestRunCompareWithPolicyFlagsAndJSON(t *testing.T) {
	baselinePath, currentPath := writeCompareFiles(t,
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.9,"passed":true,"metadata":{"case_id":"a"}}`+"\n",
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.89,"passed":true,"metadata":{"case_id":"a"}}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{
		"compare",
		"--case-id-key", "case_id",
		"--score-tolerance", "0.02",
		"--format", "json",
		baselinePath,
		currentPath,
	}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"Unchanged": 1`) || !strings.Contains(out, `"CaseName": "a"`) {
		t.Fatalf("json output missing policy comparison details:\n%s", out)
	}
}

func TestRunCompareLoadsPolicyFile(t *testing.T) {
	baselinePath, currentPath := writeCompareFiles(t,
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.9,"passed":true}`+"\n",
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.89,"passed":true}`+"\n",
	)
	policyPath := writeConfigFile(t, `{
		"default": {"score_tolerance": 0.02}
	}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"compare", "--policy", policyPath, baselinePath, currentPath}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "unchanged=1") {
		t.Fatalf("stdout missing unchanged summary:\n%s", stdout.String())
	}
}

func TestRunCompareCanDisableRegressionFailure(t *testing.T) {
	baselinePath, currentPath := writeCompareFiles(t,
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.9,"passed":true}`+"\n",
		`{"test_name":"TestEval","metric":"Faithfulness","score":0.1,"passed":false}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"compare", "--fail-on-regression=false", baselinePath, currentPath}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "regressed\tTestEval\tmetric=Faithfulness") {
		t.Fatalf("stdout missing regressed status:\n%s", stdout.String())
	}
}

func TestRunCompareReportsImprovementAndSucceeds(t *testing.T) {
	baselinePath, currentPath := writeCompareFiles(t,
		`{"test_name":"TestEval/improve","metric":"Faithfulness","score":0.4,"passed":false}`+"\n",
		`{"test_name":"TestEval/improve","metric":"Faithfulness","score":0.8,"passed":true}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"compare", baselinePath, currentPath}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "improved\tTestEval/improve\tmetric=Faithfulness") {
		t.Fatalf("stdout missing improved entry:\n%s", out)
	}
}

func TestRunSummarizeReportsMetrics(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true,"tokens":10,"latency_ns":100}`+"\n"+
			`{"test_name":"TestEval/b","metric":"Faithfulness","score":0.5,"passed":false,"tokens":30,"latency_ns":300}`+"\n"+
			`{"test_name":"TestEval/c","metric":"ToolCallF1","score":0.75,"passed":true}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"summarize", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Summary: total=3 passed=2 failed=1",
		"metric=Faithfulness\tcount=2\tpassed=1\tfailed=1\tmean_score=0.750",
		"mean_tokens=20.0\tmean_latency_ns=200",
		"metric=ToolCallF1\tcount=1\tpassed=1\tfailed=0\tmean_score=0.750",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, out)
		}
	}
}

func TestRunSummarizeReportsReliabilityGroupsAndFlakes(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/one","metric":"Faithfulness","score":1,"passed":true,"tokens":10,"latency_ns":100,"metadata":{"tier":"critical","flow":"rag.answer","dataset":"smoke/v1","case_id":"a"}}`+"\n"+
			`{"test_name":"TestEval/two","metric":"Faithfulness","score":0.5,"passed":false,"tokens":30,"latency_ns":300,"metadata":{"tier":"critical","flow":"rag.answer","dataset":"smoke/v1","case_id":"a"}}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"summarize", "--case-id-key", "case_id", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"tier=critical\tcount=2\tpassed=1\tfailed=1",
		"flow=rag.answer\tcount=2\tpassed=1\tfailed=1",
		"dataset=smoke/v1\tcount=2\tpassed=1\tfailed=1",
		"case=a/Faithfulness\tcount=2\tpassed=1\tfailed=1",
		"flaky\tcase=a\tmetric=Faithfulness\tcount=2\tpassed=1\tfailed=1",
		"mixed_pass=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, out)
		}
	}
}

func TestRunSummarizeUsesPolicyFlakyThreshold(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/one","metric":"Faithfulness","score":0.9,"passed":true,"metadata":{"case_id":"a"}}`+"\n"+
			`{"test_name":"TestEval/two","metric":"Faithfulness","score":0.7,"passed":true,"metadata":{"case_id":"a"}}`+"\n",
	)
	policyPath := writeConfigFile(t, `{
		"case_id_key": "case_id",
		"default": {"flaky_score_stddev": 0.2}
	}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"summarize", "--policy", policyPath, path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "case=a/Faithfulness\tcount=2\tpassed=2\tfailed=0") {
		t.Fatalf("stdout missing policy case summary:\n%s", out)
	}
	if strings.Contains(out, "flaky") {
		t.Fatalf("policy threshold should suppress score-only flake:\n%s", out)
	}
}

func TestRunSummarizeUsesCaseIDKeyFlag(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/old","metric":"Faithfulness","score":0.9,"passed":true,"metadata":{"case_id":"a"}}`+"\n"+
			`{"test_name":"TestEval/new","metric":"Faithfulness","score":0.8,"passed":true,"metadata":{"case_id":"a"}}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"summarize", "--case-id-key", "case_id", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "case=a/Faithfulness\tcount=2\tpassed=2\tfailed=0") {
		t.Fatalf("stdout missing stable case-id summary:\n%s", out)
	}
	if strings.Contains(out, "case=TestEval/old") || strings.Contains(out, "case=TestEval/new") {
		t.Fatalf("case-id flag should group renamed tests by case id:\n%s", out)
	}
}

func TestRunSummarizeUsesConfigPolicy(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/old","metric":"Faithfulness","score":0.9,"passed":true,"metadata":{"case_id":"a"}}`+"\n"+
			`{"test_name":"TestEval/new","metric":"Faithfulness","score":0.8,"passed":true,"metadata":{"case_id":"a"}}`+"\n",
	)
	configPath := writeConfigFile(t, `{
		"compare": {
			"case_id_key": "case_id",
			"default": {"flaky_score_stddev": 0.2}
		}
	}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"summarize", "--config", configPath, path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "case=a/Faithfulness\tcount=2\tpassed=2\tfailed=0") {
		t.Fatalf("stdout missing config case-id summary:\n%s", out)
	}
	if strings.Contains(out, "flaky") {
		t.Fatalf("config policy threshold should suppress score-only flake:\n%s", out)
	}
}

func TestRunReportWritesHTML(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true}`+"\n",
	)
	outPath := filepath.Join(t.TempDir(), "report.html")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"report", path, "--out", outPath}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile report: %v", err)
	}
	if !strings.Contains(string(data), "go-eval report") || !strings.Contains(string(data), "Faithfulness") {
		t.Fatalf("html report missing expected content:\n%s", data)
	}
	if !strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("stdout missing write confirmation: %q", stdout.String())
	}
}

func TestRunReportComparisonJSONToStdout(t *testing.T) {
	baseline := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true}`+"\n",
	)
	current := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":0,"passed":false}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"report", "--format", "json", "--baseline", baseline, "--current", current}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"Comparison"`) || !strings.Contains(out, `"Regressed": 1`) {
		t.Fatalf("json report missing comparison:\n%s", out)
	}
}

func TestRunReportMarkdownFormat(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"report", "--format", "markdown", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# go-eval report") || !strings.Contains(stdout.String(), "| Faithfulness |") {
		t.Fatalf("markdown report missing expected content:\n%s", stdout.String())
	}
}

func TestRunReportRejectsUnsupportedOutExtensionWithoutFormat(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true}`+"\n",
	)
	outPath := filepath.Join(t.TempDir(), "report.txt")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"report", path, "--out", outPath}, nil, nil, &stdout, &stderr, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--format is required") {
		t.Fatalf("stderr missing format error:\n%s", stderr.String())
	}
}

func TestRunReportFormatOverridesUnsupportedOutExtension(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"Faithfulness","score":1,"passed":true}`+"\n",
	)
	outPath := filepath.Join(t.TempDir(), "report.txt")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"report", "--format", "markdown", path, "--out", outPath}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile report: %v", err)
	}
	if !strings.Contains(string(data), "# go-eval report") {
		t.Fatalf("format override did not write markdown:\n%s", data)
	}
}

func TestRunCalibrateReportsDisagreements(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/one","metric":"Faithfulness","score":0.9,"passed":true,"metadata":{"case_id":"a","judge":"judge-a"}}`+"\n"+
			`{"test_name":"TestEval/two","metric":"Faithfulness","score":0.3,"passed":false,"metadata":{"case_id":"a","judge":"judge-b"}}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"calibrate", "--case-id-key", "case_id", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Summary: groups=1 disagreements=1 judges=2",
		"disagreement\tcase=a\tmetric=Faithfulness",
		"judge-a=0.900/true",
		"judge-b=0.300/false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, out)
		}
	}
}

func TestRunCalibrateJSONAndPairwiseVariants(t *testing.T) {
	path := writeResultFile(t,
		`{"test_name":"TestEval/a","metric":"AnswerCorrectness","score":0.9,"passed":true,"metadata":{"variant":"A"}}`+"\n"+
			`{"test_name":"TestEval/a","metric":"AnswerCorrectness","score":0.7,"passed":true,"metadata":{"variant":"B"}}`+"\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"calibrate", "--format", "json", "--pairwise-key", "variant", path}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Pairwise"`) || !strings.Contains(stdout.String(), `"Left": "A"`) {
		t.Fatalf("json calibration missing pairwise report:\n%s", stdout.String())
	}
}

func TestRunRunsListSummaryFailuresAndTrace(t *testing.T) {
	runsDir := t.TempDir()
	writeStoredRunFixture(t, runsDir, "latest-run", "2026-07-05T12:00:00Z",
		`{"test_name":"TestEval/fail","metric":"Faithfulness","trace_id":"trace-1","score":0.2,"passed":false,"reason":"missing source","metadata":{"case_id":"case-1","flow":"rag.answer","tier":"critical","dataset":"smoke/v1"}}`+"\n",
		`{"id":"trace-1","name":"rag","spans":[{"id":"span-1","name":"retrieve","kind":"tool","duration_ns":5}]}`+"\n",
	)
	if err := os.WriteFile(filepath.Join(runsDir, "latest"), []byte("latest-run\n"), 0o644); err != nil {
		t.Fatalf("WriteFile latest: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"runs", "list", "--runs-dir", runsDir, "--json"}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs list exit = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "latest-run"`) || !strings.Contains(stdout.String(), `"failed": 1`) {
		t.Fatalf("runs list json missing run summary:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"runs", "summary", "latest", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs summary exit = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Summary: total=1 passed=0 failed=1") {
		t.Fatalf("runs summary missing totals:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"runs", "failures", "latest", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs failures exit = %d; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"failure", "case=case-1", "trace_id=trace-1", "flow=rag.answer", "reason=missing source"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("runs failures missing %q in:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"runs", "trace", "latest", "--failed", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs trace exit = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "trace\tid=trace-1") || !strings.Contains(stdout.String(), "span\tid=span-1") {
		t.Fatalf("runs trace missing trace tree:\n%s", stdout.String())
	}
}

func TestRunRunsComparePreviousLatest(t *testing.T) {
	runsDir := t.TempDir()
	writeStoredRunFixture(t, runsDir, "old", "2026-07-05T10:00:00Z",
		`{"test_name":"TestEval/regress","metric":"Faithfulness","score":0.9,"passed":true}`+"\n",
		"",
	)
	writeStoredRunFixture(t, runsDir, "new", "2026-07-05T11:00:00Z",
		`{"test_name":"TestEval/regress","metric":"Faithfulness","score":0.3,"passed":false}`+"\n",
		"",
	)
	if err := os.WriteFile(filepath.Join(runsDir, "latest"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile latest: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"runs", "compare", "previous", "latest", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)

	if code != 1 {
		t.Fatalf("runs compare exit = %d, want regression failure 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "regressed\tTestEval/regress\tmetric=Faithfulness") {
		t.Fatalf("runs compare missing regression:\n%s", stdout.String())
	}
}

func TestRunRunsComparePreviousLatestRequiresTwoRuns(t *testing.T) {
	runsDir := t.TempDir()
	writeStoredRunFixture(t, runsDir, "only", "2026-07-05T10:00:00Z",
		`{"test_name":"TestEval/pass","metric":"Contains","score":1,"passed":true}`+"\n",
		"",
	)
	if err := os.WriteFile(filepath.Join(runsDir, "latest"), []byte("only\n"), 0o644); err != nil {
		t.Fatalf("WriteFile latest: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"runs", "compare", "previous", "latest", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)

	if code != 1 {
		t.Fatalf("runs compare exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "baseline: previous run is not available") {
		t.Fatalf("stderr missing previous-run error:\n%s", stderr.String())
	}
}

func TestRunRunsTraceFailedWithoutResultsReturnsEmptyList(t *testing.T) {
	runsDir := t.TempDir()
	runDir := filepath.Join(runsDir, "runs", "trace-only")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("MkdirAll run: %v", err)
	}
	tracesPath := filepath.Join(runDir, "traces.jsonl")
	if err := os.WriteFile(tracesPath, []byte(`{"id":"trace-1","name":"rag"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile traces: %v", err)
	}
	manifest := eval.NewRunManifest()
	manifest.RunID = "trace-only"
	manifest.Status = "failed"
	manifest.TracesPath = tracesPath
	manifest.ResultsPath = filepath.Join(runDir, "results.jsonl")
	manifest.StartedAt = "2026-07-05T10:00:00Z"
	if err := eval.WriteRunManifest(filepath.Join(runDir, "goeval-run.json"), manifest); err != nil {
		t.Fatalf("WriteRunManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "latest"), []byte("trace-only\n"), 0o644); err != nil {
		t.Fatalf("WriteFile latest: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"runs", "trace", "latest", "--failed", "--runs-dir", runsDir, "--json"}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("runs trace exit = %d; stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("stdout = %q, want empty JSON list", stdout.String())
	}
}

func TestRunRunsReportAndPrune(t *testing.T) {
	runsDir := t.TempDir()
	writeStoredRunFixture(t, runsDir, "old", "2026-07-05T10:00:00Z",
		`{"test_name":"TestEval/old","metric":"Contains","score":1,"passed":true}`+"\n",
		"",
	)
	writeStoredRunFixture(t, runsDir, "new", "2026-07-05T11:00:00Z",
		`{"test_name":"TestEval/new","metric":"Contains","score":1,"passed":true}`+"\n",
		"",
	)
	if err := os.WriteFile(filepath.Join(runsDir, "latest"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile latest: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "report.html")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"runs", "report", "latest", "--runs-dir", runsDir, "--out", outPath}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs report exit = %d; stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile report: %v", err)
	}
	if !strings.Contains(string(data), "go-eval report") {
		t.Fatalf("report missing content:\n%s", data)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"runs", "prune", "--keep", "1", "--runs-dir", runsDir}, nil, nil, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runs prune exit = %d; stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(runsDir, "runs", "old")); !os.IsNotExist(err) {
		t.Fatalf("old run should be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(runsDir, "runs", "new")); err != nil {
		t.Fatalf("new run should remain: %v", err)
	}
}

func TestRunVersion(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() { version = oldVersion })
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), []string{"version"}, nil, nil, &stdout, &stderr, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "goeval test-version\n" {
		t.Fatalf("stdout = %q, want version", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown command", args: []string{"wat"}, want: `unknown command "wat"`},
		{name: "compare arity", args: []string{"compare", "old.jsonl"}, want: "usage: goeval compare"},
		{name: "summarize arity", args: []string{"summarize"}, want: "usage: goeval summarize"},
		{name: "report arity", args: []string{"report"}, want: "usage: goeval report"},
		{name: "calibrate arity", args: []string{"calibrate"}, want: "usage: goeval calibrate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := run(context.Background(), tt.args, nil, nil, &stdout, &stderr, nil)

			if code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr missing %q in %q", tt.want, stderr.String())
			}
		})
	}
}

func writeResultFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "results.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile results: %v", err)
	}
	return path
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "goeval.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return path
}

func writeCompareFiles(t *testing.T, baseline string, current string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.jsonl")
	currentPath := filepath.Join(dir, "current.jsonl")
	if err := os.WriteFile(baselinePath, []byte(baseline), 0o644); err != nil {
		t.Fatalf("WriteFile baseline: %v", err)
	}
	if err := os.WriteFile(currentPath, []byte(current), 0o644); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}
	return baselinePath, currentPath
}

func writeStoredRunFixture(t *testing.T, runsDir string, id string, startedAt string, results string, traces string) {
	t.Helper()

	runDir := filepath.Join(runsDir, "runs", id)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("MkdirAll run: %v", err)
	}
	resultsPath := filepath.Join(runDir, "results.jsonl")
	if err := os.WriteFile(resultsPath, []byte(results), 0o644); err != nil {
		t.Fatalf("WriteFile results: %v", err)
	}
	tracesPath := filepath.Join(runDir, "traces.jsonl")
	if traces != "" {
		if err := os.WriteFile(tracesPath, []byte(traces), 0o644); err != nil {
			t.Fatalf("WriteFile traces: %v", err)
		}
	}
	summary, err := compareSummary(resultsPath)
	if err != nil {
		t.Fatalf("summarize fixture: %v", err)
	}
	summaryPath := filepath.Join(runDir, "summary.json")
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent summary: %v", err)
	}
	if err := os.WriteFile(summaryPath, append(summaryData, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile summary: %v", err)
	}
	reportPath := filepath.Join(runDir, "report.html")
	if err := os.WriteFile(reportPath, []byte("<html>go-eval report</html>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile report: %v", err)
	}
	manifest := eval.NewRunManifest()
	manifest.RunID = id
	manifest.GoEvalVersion = version
	manifest.Status = "passed"
	manifest.Command = []string{"go", "test", "-json", "./..."}
	manifest.ResultsPath = resultsPath
	manifest.TracesPath = tracesPath
	manifest.TestEventsPath = filepath.Join(runDir, "test-events.jsonl")
	manifest.SummaryPath = summaryPath
	manifest.ReportPath = reportPath
	manifest.StartedAt = startedAt
	manifest.EndedAt = startedAt
	if err := eval.WriteRunManifest(filepath.Join(runDir, "goeval-run.json"), manifest); err != nil {
		t.Fatalf("WriteRunManifest: %v", err)
	}
}

func compareSummary(path string) (compare.ResultsSummary, error) {
	return compare.SummarizeFile(path)
}

func envValue(env []string, key string) (string, int) {
	prefix := key + "="
	count := 0
	value := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			count++
			value = strings.TrimPrefix(entry, prefix)
		}
	}
	return value, count
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
