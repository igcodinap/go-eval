package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
