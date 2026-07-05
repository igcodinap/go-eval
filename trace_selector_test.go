package eval

import (
	"strings"
	"testing"
)

func TestTraceTextSelectors(t *testing.T) {
	trace := Trace{
		ID:   "trace-1",
		Name: "checkout",
		Metadata: map[string]any{
			"expected": "done",
		},
		Spans: []Span{
			{Name: "user", Input: "start", Output: "working"},
			{Name: "assistant", Input: "continue", Output: "done"},
		},
		Artifacts: []ArtifactRecord{{
			Key:   "state",
			Value: []byte(`{"status":"done"}`),
		}},
		StateDeltas: []StateDelta{{
			Key:   "status",
			After: []byte(`"done"`),
		}},
	}

	tests := []struct {
		name     string
		selector TraceTextSelector
		want     string
	}{
		{"trace-name", TraceName(), "checkout"},
		{"metadata", TraceMetadata("expected"), "done"},
		{"first-input", FirstSpanInput(), "start"},
		{"last-output", LastSpanOutput(), "done"},
		{"span-output", SpanOutput("user"), "working"},
		{"artifact", ArtifactValue("state"), `{"status":"done"}`},
		{"state", StateAfter("status"), `"done"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := tt.selector.SelectText(trace)
			if err != nil {
				t.Fatalf("SelectText: %v", err)
			}
			if !ok || got != tt.want {
				t.Fatalf("SelectText = %q/%t, want %q/true", got, ok, tt.want)
			}
		})
	}
}

func TestTraceCaseSelectorBuildsCase(t *testing.T) {
	trace := Trace{
		ID:       "trace-1",
		Metadata: map[string]any{"case_id": "trace-case"},
		Spans: []Span{
			{Name: "request", Input: "question"},
			{Name: "answer", Output: "answer"},
		},
	}
	selector := TraceCaseSelector{
		Input:    SpanInput("request"),
		Output:   SpanOutput("answer"),
		Expected: TraceTextSelector{Kind: TraceSelectNone},
	}

	c, err := selector.CaseFromTrace(trace)
	if err != nil {
		t.Fatalf("CaseFromTrace: %v", err)
	}
	if c.Input != "question" || c.Output != "answer" || c.TraceID != "trace-1" || c.Metadata["case_id"] != "trace-case" {
		t.Fatalf("unexpected case: %+v", c)
	}
	if c.Trace == nil || c.Trace.ID != "trace-1" {
		t.Fatalf("trace not attached: %+v", c.Trace)
	}
}

func TestTraceCaseSelectorMissingSelectorModes(t *testing.T) {
	_, err := (TraceCaseSelector{Input: SpanInput("missing")}).CaseFromTrace(Trace{})
	if err == nil {
		t.Fatalf("expected missing selector error")
	}

	c, err := (TraceCaseSelector{
		Input:     SpanInput("missing"),
		OnMissing: MissingSelectorEmpty,
	}).CaseFromTrace(Trace{})
	if err != nil {
		t.Fatalf("CaseFromTrace empty: %v", err)
	}
	if c.Input != "" {
		t.Fatalf("Input = %q, want empty", c.Input)
	}
}

func TestReadTraceJSONL(t *testing.T) {
	traces, err := ReadTraceJSONL(strings.NewReader(`{"id":"one"}

{"id":"two","name":"flow"}
`))
	if err != nil {
		t.Fatalf("ReadTraceJSONL: %v", err)
	}
	if len(traces) != 2 || traces[0].ID != "one" || traces[1].Name != "flow" {
		t.Fatalf("unexpected traces: %+v", traces)
	}
}
