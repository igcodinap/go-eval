package main

import (
	"bytes"
	"context"
	"io"
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
