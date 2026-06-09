package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type recordingTraceSink struct {
	mu     sync.Mutex
	traces []Trace
	err    error
}

func (s *recordingTraceSink) WriteTrace(trace Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces = append(s.traces, trace)
	return s.err
}

func (s *recordingTraceSink) all() []Trace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Trace(nil), s.traces...)
}

type failOnceTraceSink struct {
	mu         sync.Mutex
	attempts   int
	successful []Trace
}

func (s *failOnceTraceSink) WriteTrace(trace Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts == 1 {
		return errors.New("temporary trace sink failure")
	}
	s.successful = append(s.successful, trace)
	return nil
}

func (s *failOnceTraceSink) stats() (int, []Trace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, append([]Trace(nil), s.successful...)
}

func TestTraceJSONRoundTrip(t *testing.T) {
	trace := Trace{
		ID:   "trace-1",
		Name: "checkout",
		Spans: []Span{
			{
				ID:       "span-1",
				Name:     "charge",
				Kind:     "tool_call",
				ToolCall: &ToolCall{Name: "payments.charge", Arguments: json.RawMessage(`{"amount":42}`)},
			},
		},
		Artifacts: []ArtifactRecord{{Key: "receipt", Value: json.RawMessage(`{"id":"r1"}`)}},
		StateDeltas: []StateDelta{{
			Key:    "status",
			Before: json.RawMessage(`"pending"`),
			After:  json.RawMessage(`"paid"`),
		}},
		Metadata: map[string]any{"flow": "checkout"},
	}

	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Trace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != trace.ID || got.Spans[0].ToolCall.Name != "payments.charge" || string(got.Artifacts[0].Value) != `{"id":"r1"}` {
		t.Fatalf("unexpected trace round trip: %+v", got)
	}
}

func TestDefaultTraceSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ResultsDirEnvVar, dir)

	sink := DefaultTraceSink()
	if sink == nil {
		t.Fatalf("expected non-nil trace sink")
	}
	if err := sink.WriteTrace(Trace{ID: "trace-1", Name: "smoke"}); err != nil {
		t.Fatalf("WriteTrace: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "traces.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got Trace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != "trace-1" || got.Name != "smoke" {
		t.Fatalf("unexpected trace: %+v", got)
	}
}

func TestRunnerLinksResultRowsToTrace(t *testing.T) {
	t.Setenv(EnvVar, "1")

	resultSink := &recordingSink{}
	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(resultSink),
		WithTraceSink(traceSink),
	)

	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{
		Trace: &Trace{
			ID:   "trace-1",
			Name: "trace-name",
		},
	})

	if got := resultSink.last().TraceID; got != "trace-1" {
		t.Fatalf("result trace id = %q, want trace-1", got)
	}
	traces := traceSink.all()
	if len(traces) != 1 || traces[0].ID != "trace-1" || traces[0].TestName == "" {
		t.Fatalf("unexpected trace writes: %+v", traces)
	}
}

func TestRunnerTraceIDSeedsEmptyTraceID(t *testing.T) {
	t.Setenv(EnvVar, "1")

	resultSink := &recordingSink{}
	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(resultSink),
		WithTraceSink(traceSink),
	)

	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{
		TraceID: "external-trace",
		Trace:   &Trace{Name: "trace-name"},
	})

	if got := resultSink.last().TraceID; got != "external-trace" {
		t.Fatalf("result trace id = %q, want external-trace", got)
	}
	traces := traceSink.all()
	if len(traces) != 1 || traces[0].ID != "external-trace" {
		t.Fatalf("unexpected trace writes: %+v", traces)
	}
}

func TestRunnerTraceIDPrefersTraceWhenBothSet(t *testing.T) {
	t.Setenv(EnvVar, "1")

	resultSink := &recordingSink{}
	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(resultSink),
		WithTraceSink(traceSink),
	)

	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{
		TraceID: "external-trace",
		Trace:   &Trace{ID: "embedded-trace", Name: "trace-name"},
	})

	if got := resultSink.last().TraceID; got != "embedded-trace" {
		t.Fatalf("result trace id = %q, want embedded-trace", got)
	}
	traces := traceSink.all()
	if len(traces) != 1 || traces[0].ID != "embedded-trace" {
		t.Fatalf("unexpected trace writes: %+v", traces)
	}
}

func TestRunnerWritesSharedTraceOnce(t *testing.T) {
	t.Setenv(EnvVar, "1")

	resultSink := &recordingSink{}
	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(resultSink),
		WithTraceSink(traceSink),
	)
	c := Case{Trace: &Trace{ID: "trace-1", Name: "shared"}}

	_ = r.Run(t, scriptedMetric{
		name:   "A",
		result: Result{Score: 1, Passed: true, Metric: "A", Reason: "ok"},
	}, c)
	_ = r.Run(t, scriptedMetric{
		name:   "B",
		result: Result{Score: 1, Passed: true, Metric: "B", Reason: "ok"},
	}, c)

	results := resultSink.all()
	if len(results) != 2 || results[0].TraceID != "trace-1" || results[1].TraceID != "trace-1" {
		t.Fatalf("result rows missing shared trace id: %+v", results)
	}
	if traces := traceSink.all(); len(traces) != 1 {
		t.Fatalf("expected one trace write for shared trace id, got %+v", traces)
	}
}

func TestRunnerRetriesTraceWriteAfterSinkFailure(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	traceSink := &failOnceTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithTraceSink(traceSink),
	)
	c := Case{Trace: &Trace{ID: "trace-1", Spans: []Span{{Name: "lookup"}}}}

	_ = r.Run(tb, scriptedMetric{
		name:   "A",
		result: Result{Score: 1, Passed: true, Metric: "A", Reason: "ok"},
	}, c)
	if !tb.errored {
		t.Fatalf("expected first trace sink failure to call Errorf")
	}

	tb.errored = false
	_ = r.Run(tb, scriptedMetric{
		name:   "B",
		result: Result{Score: 1, Passed: true, Metric: "B", Reason: "ok"},
	}, c)
	attempts, successful := traceSink.stats()
	if attempts != 2 || len(successful) != 1 {
		t.Fatalf("expected retry to write once successfully, attempts=%d successful=%+v", attempts, successful)
	}

	_ = r.Run(tb, scriptedMetric{
		name:   "C",
		result: Result{Score: 1, Passed: true, Metric: "C", Reason: "ok"},
	}, c)
	attempts, successful = traceSink.stats()
	if attempts != 2 || len(successful) != 1 {
		t.Fatalf("expected successful trace id to be deduped, attempts=%d successful=%+v", attempts, successful)
	}
}

func TestRunnerRedactsTraceWrites(t *testing.T) {
	t.Setenv(EnvVar, "1")

	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithTraceSink(traceSink),
		WithRedactors(func(path string, value string) string {
			if finalPathSegment(path) == "secret" {
				return "[REDACTED]"
			}
			return value
		}),
	)

	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{
		Trace: &Trace{
			ID: "trace-1",
			Spans: []Span{{
				Name:   "lookup",
				Output: "public",
				Metadata: map[string]any{
					"secret": "hide-me",
				},
			}},
		},
	})

	traces := traceSink.all()
	if got := traces[0].Spans[0].Metadata["secret"]; got != "[REDACTED]" {
		t.Fatalf("trace metadata was not redacted: %+v", traces[0].Spans[0].Metadata)
	}
}

func TestRunnerRedactsTraceWithoutMutatingOriginal(t *testing.T) {
	t.Setenv(EnvVar, "1")

	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithTraceSink(traceSink),
		WithRedactors(func(path string, value string) string {
			if finalPathSegment(path) == "secret" {
				return "[REDACTED]"
			}
			return value
		}),
	)
	trace := &Trace{
		ID: "trace-1",
		Spans: []Span{{
			Name: "lookup",
			Metadata: map[string]any{
				"secret": "hide-me",
			},
		}},
		StateDeltas: []StateDelta{{
			Key:   "state",
			After: json.RawMessage(`{"secret":"hide-me"}`),
		}},
	}

	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 1, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{Trace: trace})

	if got := trace.Spans[0].Metadata["secret"]; got != "hide-me" {
		t.Fatalf("original trace metadata was mutated: %+v", trace.Spans[0].Metadata)
	}
	if got := string(trace.StateDeltas[0].After); got != `{"secret":"hide-me"}` {
		t.Fatalf("original trace state delta was mutated: %s", got)
	}
	traces := traceSink.all()
	var redacted map[string]any
	if err := json.Unmarshal(traces[0].StateDeltas[0].After, &redacted); err != nil {
		t.Fatalf("Unmarshal redacted state delta: %v", err)
	}
	if got := redacted["secret"]; got != "[REDACTED]" {
		t.Fatalf("trace state delta was not redacted: %+v", redacted)
	}
}

func TestRunScenarioEmitsTraceID(t *testing.T) {
	t.Setenv(EnvVar, "1")

	resultSink := &recordingSink{}
	traceSink := &recordingTraceSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(resultSink),
		WithTraceSink(traceSink),
	)

	got := r.RunScenario(t, Scenario{
		Name: "checkout",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{
				Output: "done",
				Turns: []Turn{{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{{
						Name: "charge",
					}},
				}},
				Artifacts: map[string]json.RawMessage{"receipt": json.RawMessage(`{"ok":true}`)},
			}, nil
		},
		Steps: []Step{{Name: "pay", RequiredTools: []string{"charge"}}},
	})

	if got.TraceID == "" {
		t.Fatalf("scenario result missing trace id")
	}
	results := resultSink.all()
	if len(results) < 2 || results[0].TraceID != got.TraceID || results[len(results)-1].TraceID != got.TraceID {
		t.Fatalf("result rows missing scenario trace id: %+v", results)
	}
	traces := traceSink.all()
	if len(traces) != 1 || traces[0].ScenarioName != "checkout" || len(traces[0].Spans) < 2 {
		t.Fatalf("unexpected scenario traces: %+v", traces)
	}
}
