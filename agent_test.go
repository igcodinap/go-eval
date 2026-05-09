package eval

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedAgentMetric struct {
	name   string
	result Result
	err    error
	calls  int
}

func (m *scriptedAgentMetric) Name() string { return m.name }

func (m *scriptedAgentMetric) ScoreAgent(ctx context.Context, j Judge, c AgentCase) (Result, error) {
	m.calls++
	return m.result, m.err
}

func TestRunner_RunAgentSkipsWhenGoevalUnset(t *testing.T) {
	t.Setenv(EnvVar, "")

	tb := &recordingTB{}
	metric := &scriptedAgentMetric{name: "AgentX", result: Result{Score: 1, Passed: true}}
	r := NewRunner(&MockJudge{})

	_ = r.RunAgent(tb, metric, AgentCase{})

	if !tb.skipped {
		t.Fatalf("expected Skip when GOEVAL unset")
	}
	if metric.calls != 0 {
		t.Fatalf("expected skipped case not to call metric, got %d calls", metric.calls)
	}
}

func TestRunner_RunAgentWithCaseFilterSkipsUnmatchedCase(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	metric := &scriptedAgentMetric{name: "AgentX", result: Result{Score: 1, Passed: true}}
	r := NewRunner(&MockJudge{}, WithCaseFilter(func(c Case) bool {
		return c.Metadata["tier"] == "critical"
	}))

	got := r.RunAgent(tb, metric, AgentCase{Metadata: map[string]any{"tier": "standard"}})

	if !tb.skipped {
		t.Fatalf("expected case filter to skip")
	}
	if metric.calls != 0 {
		t.Fatalf("expected skipped case not to call metric, got %d calls", metric.calls)
	}
	if got.Score != 0 || got.Passed || got.Metadata != nil {
		t.Fatalf("expected zero result for skipped case, got %+v", got)
	}
}

func TestRunner_RunAgentCopiesAgentMetadataToResultAndSink(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	c := AgentCase{Metadata: map[string]any{
		"flow":     "support.lookup",
		"tier":     "critical",
		"trace_id": "trace-123",
	}}

	got := r.RunAgent(t, &scriptedAgentMetric{
		name:   "AgentX",
		result: Result{Score: 0.9, Passed: true, Reason: "ok"},
	}, c)

	if got.Metadata["trace_id"] != "trace-123" {
		t.Fatalf("expected copied metadata, got %+v", got.Metadata)
	}
	got.Metadata["trace_id"] = "changed"
	if c.Metadata["trace_id"] != "trace-123" {
		t.Fatalf("metadata was not copied: %+v", c.Metadata)
	}
	if sink.count() != 1 {
		t.Fatalf("expected one sink write, got %d", sink.count())
	}
	if sink.last().Metadata["flow"] != "support.lookup" {
		t.Fatalf("unexpected sink metadata: %+v", sink.last().Metadata)
	}
}

func TestRunner_RunAgentPreservesMetricMetadata(t *testing.T) {
	t.Setenv(EnvVar, "1")

	r := NewRunner(&MockJudge{})
	got := r.RunAgent(t, &scriptedAgentMetric{
		name: "AgentX",
		result: Result{
			Score:    1,
			Passed:   true,
			Metadata: map[string]any{"metric": "kept"},
		},
	}, AgentCase{Metadata: map[string]any{"case": "ignored"}})

	if got.Metadata["metric"] != "kept" {
		t.Fatalf("expected metric metadata to be preserved, got %+v", got.Metadata)
	}
	if _, ok := got.Metadata["case"]; ok {
		t.Fatalf("did not expect case metadata merge, got %+v", got.Metadata)
	}
}

func TestRunner_RunAgentFillsMetricNameAndLatency(t *testing.T) {
	t.Setenv(EnvVar, "1")

	r := NewRunner(&MockJudge{})
	got := r.RunAgent(t, &scriptedAgentMetric{
		name:   "AgentX",
		result: Result{Score: 1, Passed: true},
	}, AgentCase{})

	if got.Metric != "AgentX" {
		t.Fatalf("Metric = %q, want AgentX", got.Metric)
	}
	if got.Latency <= 0 {
		t.Fatalf("expected latency to be filled, got %s", got.Latency)
	}
}

func TestRunner_RunAgentFatalsOnMetricError(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	_ = r.RunAgent(tb, &scriptedAgentMetric{name: "AgentX", err: errors.New("boom")}, AgentCase{})

	if !tb.fataled {
		t.Fatalf("expected Fatalf on metric error")
	}
}

func TestRunner_RunAgentErrorsOnLowScore(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	r := NewRunner(&MockJudge{})
	_ = r.RunAgent(tb, &scriptedAgentMetric{
		name:   "AgentX",
		result: Result{Score: 0.4, Passed: false, Reason: "bad"},
	}, AgentCase{})

	if !tb.errored {
		t.Fatalf("expected Errorf when result.Passed == false")
	}
}

func TestAgentCaseViewCarriesSharedFields(t *testing.T) {
	c := AgentCase{
		Output:   "done",
		Expected: "done well",
		Context:  []string{"ctx"},
		Metadata: map[string]any{"tier": "critical"},
	}

	got := c.caseView()

	if got.Output != c.Output || got.Expected != c.Expected || got.Context[0] != "ctx" ||
		got.Metadata["tier"] != "critical" {
		t.Fatalf("unexpected case view: %+v", got)
	}
}

func TestTraceSpanLatencyIsDuration(t *testing.T) {
	span := TraceSpan{Latency: 50 * time.Millisecond}
	if span.Latency != 50*time.Millisecond {
		t.Fatalf("unexpected latency: %s", span.Latency)
	}
}
