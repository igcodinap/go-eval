package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

type countingMetric struct {
	name   string
	result Result
	calls  int
}

func (m *countingMetric) Name() string { return m.name }

func (m *countingMetric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	_ = ctx
	_ = j
	_ = c
	m.calls++
	return m.result, nil
}

func TestPrecheck_FailedPrecheckSkipsMain(t *testing.T) {
	pre := &countingMetric{
		name: "Contains",
		result: Result{
			Score:            0,
			Passed:           false,
			Metric:           "Contains",
			Reason:           "missing city",
			Tokens:           2,
			PromptTokens:     1,
			CompletionTokens: 1,
			Latency:          5 * time.Millisecond,
		},
	}
	main := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Reason: "ok"},
	}

	r, err := (Precheck{Pre: pre, Main: main}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if pre.calls != 1 {
		t.Fatalf("expected pre to run once, got %d", pre.calls)
	}
	if main.calls != 0 {
		t.Fatalf("expected main to be skipped, got %d calls", main.calls)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(r.Reason, "precheck<Contains> failed:") {
		t.Fatalf("reason missing precheck prefix: %q", r.Reason)
	}
	if r.Tokens != 2 || r.Latency != 5*time.Millisecond {
		t.Fatalf("unexpected metadata aggregation: tokens=%d latency=%s", r.Tokens, r.Latency)
	}
	if r.PromptTokens != 1 || r.CompletionTokens != 1 {
		t.Fatalf("unexpected token split: prompt=%d completion=%d", r.PromptTokens, r.CompletionTokens)
	}
}

func TestPrecheck_PassingPrecheckRunsMain(t *testing.T) {
	pre := &countingMetric{
		name:   "Contains",
		result: Result{Score: 1, Passed: true, Metric: "Contains", Tokens: 2, PromptTokens: 1, CompletionTokens: 1, Latency: 5 * time.Millisecond},
	}
	main := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Tokens: 10, PromptTokens: 4, CompletionTokens: 6, Latency: 20 * time.Millisecond},
	}

	r, err := (Precheck{Pre: pre, Main: main}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if pre.calls != 1 || main.calls != 1 {
		t.Fatalf("unexpected calls pre=%d main=%d", pre.calls, main.calls)
	}
	if !r.Passed || r.Score != 0.9 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Tokens != 12 {
		t.Fatalf("unexpected token aggregation: got %d", r.Tokens)
	}
	if r.PromptTokens != 5 || r.CompletionTokens != 7 {
		t.Fatalf("unexpected token split aggregation: prompt=%d completion=%d", r.PromptTokens, r.CompletionTokens)
	}
	if r.Latency != 25*time.Millisecond {
		t.Fatalf("unexpected latency aggregation: got %s", r.Latency)
	}
}
