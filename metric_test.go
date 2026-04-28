package eval

import (
	"context"
	"testing"
	"time"
)

type fakeMetric struct{ name string }

func (m fakeMetric) Name() string { return m.name }

func (m fakeMetric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return Result{Score: 1.0, Passed: true, Metric: m.name}, nil
}

func TestMetric_InterfaceSatisfied(t *testing.T) {
	var m Metric = fakeMetric{name: "Fake"}

	if m.Name() != "Fake" {
		t.Fatalf("Name: got %q", m.Name())
	}
	r, err := m.Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 || r.Metric != "Fake" {
		t.Fatalf("unexpected Result: %+v", r)
	}
}

func TestResult_FieldsAccessible(t *testing.T) {
	r := Result{
		Score:            0.75,
		Reason:           "ok",
		Passed:           true,
		Metric:           "Faithfulness",
		Latency:          100 * time.Millisecond,
		Tokens:           42,
		PromptTokens:     20,
		CompletionTokens: 22,
	}
	if r.Score != 0.75 || r.Tokens != 42 || r.PromptTokens != 20 || r.CompletionTokens != 22 || r.Latency != 100*time.Millisecond {
		t.Fatalf("unexpected Result: %+v", r)
	}
}

func TestRunPromptMetricCopiesTokenSplit(t *testing.T) {
	j := &MockJudge{Response: JudgeResponse{
		Score:            0.8,
		Reason:           "ok",
		Tokens:           10,
		PromptTokens:     4,
		CompletionTokens: 6,
	}}

	r, err := runPromptMetric(context.Background(), j, "prompt", "faithfulness", "Faithfulness", 0.7)
	if err != nil {
		t.Fatalf("runPromptMetric: %v", err)
	}
	if r.Tokens != 10 || r.PromptTokens != 4 || r.CompletionTokens != 6 {
		t.Fatalf("unexpected token fields: %+v", r)
	}
}
