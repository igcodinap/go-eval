package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenBudget_PassesWithinBudget(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Reason: "ok", Tokens: 99},
	}

	r, err := WithTokenBudget(100, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Tokens != 99 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestTokenBudget_FailsOverBudget(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Reason: "ok", Tokens: 101},
	}

	r, err := WithTokenBudget(100, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected budget failure, got %+v", r)
	}
	if r.Score != 0.9 || r.Tokens != 101 {
		t.Fatalf("budget wrapper should preserve score and tokens, got %+v", r)
	}
	if !strings.Contains(r.Reason, "token budget exceeded: 101 > 100") ||
		!strings.Contains(r.Reason, "ok") {
		t.Fatalf("unexpected reason: %q", r.Reason)
	}
}

func TestTokenBudget_DisabledWhenMaxNonPositive(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Tokens: 101},
	}

	r, err := WithTokenBudget(0, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected disabled budget to pass, got %+v", r)
	}
}

func TestTokenBudget_ExactBoundaryPasses(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Tokens: 100},
	}

	r, err := WithTokenBudget(100, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected exact budget boundary to pass, got %+v", r)
	}
}

func TestLatencyBudget_PassesWithinBudget(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Latency: 50 * time.Millisecond},
	}

	r, err := WithLatencyBudget(100*time.Millisecond, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Latency != 50*time.Millisecond {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestLatencyBudget_DisabledWhenMaxNonPositive(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Latency: 101 * time.Millisecond},
	}

	r, err := WithLatencyBudget(0, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected disabled budget to pass, got %+v", r)
	}
}

func TestLatencyBudget_FailsOverBudget(t *testing.T) {
	metric := &countingMetric{
		name:   "Faithfulness",
		result: Result{Score: 0.9, Passed: true, Metric: "Faithfulness", Reason: "ok", Latency: 101 * time.Millisecond},
	}

	r, err := WithLatencyBudget(100*time.Millisecond, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected budget failure, got %+v", r)
	}
	if !strings.Contains(r.Reason, "latency budget exceeded: 101ms > 100ms") ||
		!strings.Contains(r.Reason, "ok") {
		t.Fatalf("unexpected reason: %q", r.Reason)
	}
}

func TestLatencyBudget_FillsMissingLatency(t *testing.T) {
	metric := funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
		_ = ctx
		_ = j
		_ = c
		time.Sleep(2 * time.Millisecond)
		return Result{Score: 1, Passed: true, Metric: "X"}, nil
	})

	r, err := WithLatencyBudget(time.Second, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Latency <= 0 {
		t.Fatalf("expected wrapper to fill latency, got %+v", r)
	}
}

func TestBudgetWrappersPropagateInnerMetricErrors(t *testing.T) {
	wantErr := errors.New("judge exploded")
	metric := scriptedMetric{
		name:   "Faithfulness",
		result: Result{Metric: "Faithfulness", Tokens: 999, Latency: time.Second},
		err:    wantErr,
	}

	tokenResult, err := WithTokenBudget(1, metric).Score(context.Background(), nil, Case{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected token budget to propagate inner error, got %v", err)
	}
	if tokenResult.Passed {
		t.Fatalf("unexpected passing result: %+v", tokenResult)
	}
	if strings.Contains(tokenResult.Reason, "budget exceeded") {
		t.Fatalf("budget should not rewrite inner error result, got %+v", tokenResult)
	}

	latencyResult, err := WithLatencyBudget(time.Millisecond, metric).Score(context.Background(), nil, Case{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected latency budget to propagate inner error, got %v", err)
	}
	if latencyResult.Passed {
		t.Fatalf("unexpected passing result: %+v", latencyResult)
	}
	if strings.Contains(latencyResult.Reason, "budget exceeded") {
		t.Fatalf("budget should not rewrite inner error result, got %+v", latencyResult)
	}
}

func TestBudgetWrappersRejectNilMetric(t *testing.T) {
	if _, err := WithTokenBudget(10, nil).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected token budget nil metric error")
	}
	if _, err := WithLatencyBudget(time.Second, nil).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected latency budget nil metric error")
	}
}

func TestOutputLengthBudget(t *testing.T) {
	tests := []struct {
		name   string
		metric OutputLengthBudget
		output string
		want   bool
	}{
		{name: "disabled", metric: OutputLengthBudget{}, output: "anything", want: true},
		{name: "under rune budget", metric: OutputLengthBudget{MaxRunes: 4}, output: "café", want: true},
		{name: "over rune budget", metric: OutputLengthBudget{MaxRunes: 3}, output: "café", want: false},
		{name: "over word budget", metric: OutputLengthBudget{MaxWords: 2}, output: "uno dos tres", want: false},
		{name: "both budgets pass", metric: OutputLengthBudget{MaxRunes: 20, MaxWords: 3}, output: "uno dos", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.metric.Score(context.Background(), nil, Case{Output: tt.output})
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if got.Passed != tt.want {
				t.Fatalf("unexpected result: %+v", got)
			}
		})
	}
}
