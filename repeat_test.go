package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRepeatAggregatesResults(t *testing.T) {
	metric := &sequenceMetric{
		name: "Faithfulness",
		results: []Result{
			{Score: 1.0, Passed: true, Tokens: 10, PromptTokens: 4, CompletionTokens: 6, Latency: 10 * time.Millisecond},
			{Score: 0.5, Passed: false, Tokens: 12, PromptTokens: 5, CompletionTokens: 7, Latency: 20 * time.Millisecond},
			{Score: 1.0, Passed: true, Tokens: 14, PromptTokens: 6, CompletionTokens: 8, Latency: 30 * time.Millisecond},
		},
	}

	result, err := (Repeat{Metric: metric, N: 3, PassRate: 2.0 / 3.0}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected repeat pass, got %+v", result)
	}
	if result.Score != 2.5/3.0 {
		t.Fatalf("score = %v, want mean", result.Score)
	}
	if result.Tokens != 36 || result.PromptTokens != 15 || result.CompletionTokens != 21 {
		t.Fatalf("unexpected token totals: %+v", result)
	}
	if result.Latency != 60*time.Millisecond {
		t.Fatalf("latency = %s, want 60ms", result.Latency)
	}
	if len(result.Dimensions) != 5 || result.Dimensions[0].Name != "pass_rate" || result.Dimensions[0].Score != 2.0/3.0 {
		t.Fatalf("unexpected dimensions: %+v", result.Dimensions)
	}
}

func TestRepeatDefaultsToAllPassRequired(t *testing.T) {
	metric := &sequenceMetric{
		name: "Faithfulness",
		results: []Result{
			{Score: 1, Passed: true},
			{Score: 0, Passed: false},
		},
	}

	result, err := RepeatN(2, metric).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected default all-pass requirement to fail, got %+v", result)
	}
}

func TestRepeatRejectsInvalidConfig(t *testing.T) {
	if _, err := (Repeat{Metric: fakeMetric{name: "X"}, N: 1}).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected invalid N error")
	}
	if _, err := (Repeat{Metric: fakeMetric{name: "X"}, N: 2, PassRate: 1.1}).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected invalid pass rate error")
	}
	if _, err := (Repeat{N: 2}).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected nil metric error")
	}
}

func TestRepeatPropagatesInnerError(t *testing.T) {
	wantErr := errors.New("judge failed")
	metric := &sequenceMetric{
		name:    "Faithfulness",
		results: []Result{{Score: 1, Passed: true}, {Score: 0, Passed: false}},
		errs:    []error{nil, wantErr},
	}

	result, err := (Repeat{Metric: metric, N: 2}).Score(context.Background(), nil, Case{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped inner error, got %v", err)
	}
	if result.Metric != "Repeat(Faithfulness)" || result.Passed || result.Score != 0 || !strings.Contains(result.Reason, "run 2 failed before repeat aggregation") {
		t.Fatalf("unexpected error result: %+v", result)
	}
}

type sequenceMetric struct {
	name    string
	results []Result
	errs    []error
	calls   int
}

func (m *sequenceMetric) Name() string {
	return m.name
}

func (m *sequenceMetric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	_ = ctx
	_ = j
	_ = c

	idx := m.calls
	m.calls++
	result := m.results[idx]
	result.Metric = m.name
	var err error
	if idx < len(m.errs) {
		err = m.errs[idx]
	}
	return result, err
}
