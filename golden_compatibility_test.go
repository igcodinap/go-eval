package eval

import (
	"strings"
	"testing"
)

func TestReadTraceJSONL_PreV11TraceRows(t *testing.T) {
	traces, err := ReadTraceJSONL(strings.NewReader(`{"id":"trace-1","name":"checkout","test_name":"TestScenario","started_at":"2026-06-10T00:00:00Z","ended_at":"2026-06-10T00:00:01Z","duration_ns":1000,"spans":[{"id":"span-1","name":"search","kind":"tool_call","input":"find route","output":"route found","tool_call":{"name":"search","arguments":{"query":"route"},"result":"ok"}}],"artifacts":[{"key":"state","value":{"status":"ready"}}],"state_deltas":[{"key":"status","before":"pending","after":"ready"}],"metadata":{"case_id":"route-1"}}
`))
	if err != nil {
		t.Fatalf("ReadTraceJSONL: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	trace := traces[0]
	if trace.ID != "trace-1" || trace.Name != "checkout" || trace.Metadata["case_id"] != "route-1" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
	if len(trace.Spans) != 1 || trace.Spans[0].ToolCall == nil || trace.Spans[0].ToolCall.Name != "search" {
		t.Fatalf("unexpected spans: %+v", trace.Spans)
	}
	if len(trace.Artifacts) != 1 || string(trace.Artifacts[0].Value) != `{"status":"ready"}` {
		t.Fatalf("unexpected artifacts: %+v", trace.Artifacts)
	}
}
