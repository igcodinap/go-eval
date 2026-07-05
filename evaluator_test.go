package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluatorEvaluateNamedWritesResultSink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	evaluator := NewEvaluator(nil, WithEvaluatorResultSink(NewJSONLResultSink(path)))
	c := Case{
		Output:   "Paris is the capital of France",
		Expected: "Paris",
		Metadata: map[string]any{"case_id": "capital/fr"},
	}

	result, err := evaluator.EvaluateNamed(context.Background(), "TestPostHoc/france", Contains{}, c)
	if err != nil {
		t.Fatalf("EvaluateNamed: %v", err)
	}
	if !result.Passed || result.Metadata["case_id"] != "capital/fr" {
		t.Fatalf("unexpected result: %+v", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"test_name":"TestPostHoc/france"`) ||
		!strings.Contains(string(data), `"metric":"Contains"`) ||
		!strings.Contains(string(data), `"case_id":"capital/fr"`) {
		t.Fatalf("unexpected JSONL: %s", data)
	}
}

func TestEvaluatorReturnsMetricError(t *testing.T) {
	sentinel := errors.New("metric failed")
	evaluator := NewEvaluator(nil)

	_, err := evaluator.Evaluate(context.Background(), funcMetric(
		func(context.Context, Judge, Case) (Result, error) {
			return Result{Metric: "broken"}, sentinel
		},
	), Case{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Evaluate err = %v, want %v", err, sentinel)
	}
}

func TestEvaluatorWritesTraceSinkOnce(t *testing.T) {
	sink := &evaluatorRecordingTraceSink{}
	evaluator := NewEvaluator(nil, WithEvaluatorTraceSink(sink))
	c := Case{
		Trace: &Trace{
			ID:   "trace-1",
			Name: "flow",
		},
		Output:   "ok",
		Expected: "ok",
	}

	if _, err := evaluator.EvaluateNamed(context.Background(), "TestTrace/one", Contains{}, c); err != nil {
		t.Fatalf("EvaluateNamed first: %v", err)
	}
	if _, err := evaluator.EvaluateNamed(context.Background(), "TestTrace/two", Contains{}, c); err != nil {
		t.Fatalf("EvaluateNamed second: %v", err)
	}
	if len(sink.traces) != 1 {
		t.Fatalf("trace writes = %d, want 1", len(sink.traces))
	}
}

type evaluatorRecordingTraceSink struct {
	traces []Trace
}

func (s *evaluatorRecordingTraceSink) WriteTrace(trace Trace) error {
	s.traces = append(s.traces, trace)
	return nil
}
