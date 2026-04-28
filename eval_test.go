package eval

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type recordingTB struct {
	testing.TB

	helperCalls int
	skipped     bool
	errored     bool
	fataled     bool
	logs        []string
	errMsgs     []string
	fatalMsgs   []string
	skipMsgs    []string
}

func (r *recordingTB) Helper() { r.helperCalls++ }
func (r *recordingTB) Skip(args ...any) {
	r.skipped = true
	r.skipMsgs = append(r.skipMsgs, fmt.Sprint(args...))
}
func (r *recordingTB) SkipNow() { r.skipped = true }
func (r *recordingTB) Errorf(format string, args ...any) {
	r.errored = true
	r.errMsgs = append(r.errMsgs, format)
}
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fataled = true
	r.fatalMsgs = append(r.fatalMsgs, format)
}
func (r *recordingTB) Logf(format string, args ...any) { r.logs = append(r.logs, format) }
func (r *recordingTB) Name() string                    { return "recording" }
func (r *recordingTB) Failed() bool                    { return r.errored || r.fataled }

type scriptedMetric struct {
	name   string
	result Result
	err    error
}

func (m scriptedMetric) Name() string { return m.name }

func (m scriptedMetric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return m.result, m.err
}

func TestRunner_SkipsWhenGoevalUnset(t *testing.T) {
	t.Setenv("GOEVAL", "")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	_ = r.Run(tb, scriptedMetric{name: "X", result: Result{Score: 1, Passed: true}}, Case{})

	if !tb.skipped {
		t.Fatalf("expected Skip when GOEVAL unset")
	}
}

func TestRunner_PassesWhenResultPassed(t *testing.T) {
	t.Setenv("GOEVAL", "1")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	result := Result{Score: 0.9, Passed: true, Metric: "X", Reason: "good"}
	got := r.Run(tb, scriptedMetric{name: "X", result: result}, Case{})

	if tb.errored || tb.fataled {
		t.Fatalf("did not expect failure, errMsgs=%v fatalMsgs=%v", tb.errMsgs, tb.fatalMsgs)
	}
	if len(tb.logs) == 0 {
		t.Fatalf("expected a Logf on pass")
	}
	if got.Score != 0.9 {
		t.Fatalf("Run should return Result, got %+v", got)
	}
}

func TestRunner_FailsWhenResultNotPassed(t *testing.T) {
	t.Setenv("GOEVAL", "1")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	result := Result{Score: 0.4, Passed: false, Metric: "X", Reason: "bad"}
	_ = r.Run(tb, scriptedMetric{name: "X", result: result}, Case{})

	if !tb.errored {
		t.Fatalf("expected Errorf when result.Passed == false")
	}
}

func TestRunner_FatalsOnMetricError(t *testing.T) {
	t.Setenv("GOEVAL", "1")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	_ = r.Run(tb, scriptedMetric{name: "X", err: errors.New("boom")}, Case{})

	if !tb.fataled {
		t.Fatalf("expected Fatalf on metric error")
	}
}

func TestRunner_WithTimeoutAppliesContext(t *testing.T) {
	t.Setenv("GOEVAL", "1")

	var gotDeadline bool
	sm := funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
		_, gotDeadline = ctx.Deadline()
		return Result{Score: 1, Passed: true, Metric: "X"}, nil
	})

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithTimeout(50*time.Millisecond))
	_ = r.Run(tb, sm, Case{})

	if !gotDeadline {
		t.Fatalf("expected metric to receive a context with deadline")
	}
}

func TestRunner_WithCaseFilterSkipsUnmatchedCase(t *testing.T) {
	t.Setenv(EnvVar, "1")

	metric := &countingMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X"},
	}
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithCaseFilter(func(c Case) bool {
		return c.Metadata["tier"] == "critical"
	}))

	got := r.Run(tb, metric, Case{Metadata: map[string]any{"tier": "standard"}})

	if !tb.skipped {
		t.Fatalf("expected case filter to skip")
	}
	if len(tb.skipMsgs) != 1 || tb.skipMsgs[0] != "eval skipped by case filter" {
		t.Fatalf("unexpected skip messages: %v", tb.skipMsgs)
	}
	if metric.calls != 0 {
		t.Fatalf("expected skipped case not to call metric, got %d calls", metric.calls)
	}
	if got.Score != 0 || got.Passed || got.Metric != "" || got.Reason != "" ||
		got.Tokens != 0 || got.PromptTokens != 0 || got.CompletionTokens != 0 ||
		got.Metadata != nil {
		t.Fatalf("expected zero result for skipped case, got %+v", got)
	}
}

func TestRunner_WithCaseFilterRunsMatchedCase(t *testing.T) {
	t.Setenv(EnvVar, "1")

	metric := &countingMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X"},
	}
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithCaseFilter(func(c Case) bool {
		return c.Metadata["tier"] == "critical"
	}))

	got := r.Run(tb, metric, Case{Metadata: map[string]any{"tier": "critical"}})

	if tb.skipped {
		t.Fatalf("did not expect matched case to skip")
	}
	if metric.calls != 1 {
		t.Fatalf("expected metric to run once, got %d calls", metric.calls)
	}
	if !got.Passed {
		t.Fatalf("expected passing result, got %+v", got)
	}
}

type funcMetric func(ctx context.Context, j Judge, c Case) (Result, error)

func (f funcMetric) Name() string { return "FuncMetric" }

func (f funcMetric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return f(ctx, j, c)
}
