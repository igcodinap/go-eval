package compare

import (
	"strings"
	"testing"
)

func TestReadJSONL_PreV11ResultRows(t *testing.T) {
	rows, err := ReadJSONL(strings.NewReader(`{"timestamp":"2026-06-10T00:00:00Z","test_name":"TestRAG/france","metric":"Faithfulness","trace_id":"trace-1","score":0.91,"passed":true,"reason":"supported","tokens":42,"prompt_tokens":30,"completion_tokens":12,"latency_ns":1000,"metadata":{"case_id":"rag/france","tier":"critical"}}
{"timestamp":"2026-06-10T00:00:01Z","test_name":"TestRAG/france","metric":"Compound","score":0.8,"passed":true,"reason":"ok","tokens":10,"latency_ns":2000,"dimensions":[{"name":"grounded","score":0.8,"threshold":0.7,"passed":true,"reason":"ok"}]}
`))
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].TraceID != "trace-1" || rows[0].Metadata["case_id"] != "rag/france" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if len(rows[1].Dimensions) != 1 || rows[1].Dimensions[0].Name != "grounded" {
		t.Fatalf("unexpected dimensions: %+v", rows[1].Dimensions)
	}
}
