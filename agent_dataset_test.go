package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAgentCasesLoadsJSONDataset(t *testing.T) {
	cases, err := DecodeAgentCases(strings.NewReader(`{
		"agent_cases": [
			{
				"name": "order-status",
				"messages": [
					{"role": "user", "content": "Where is my order?"}
				],
				"output": "Your order arrives tomorrow.",
				"expected": "Answer with delivery status.",
				"context": ["Order 42 delivery date: tomorrow."],
				"trace": [
					{
						"kind": "tool",
						"name": "orders.lookup",
						"input": "order_id=42",
						"output": "delivery_date=tomorrow",
						"metadata": {"cache_hit": true}
					}
				],
				"metadata": {
					"flow": "support.lookup",
					"tier": "critical",
					"dataset": "support/smoke-v1"
				}
			}
		]
	}`))
	if err != nil {
		t.Fatalf("DecodeAgentCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one case, got %d", len(cases))
	}

	got := cases[0]
	if len(got.Messages) != 1 || got.Messages[0].Role != RoleUser ||
		got.Messages[0].Content != "Where is my order?" {
		t.Fatalf("unexpected messages: %+v", got.Messages)
	}
	if got.Output != "Your order arrives tomorrow." || got.Expected != "Answer with delivery status." {
		t.Fatalf("unexpected output/expected: %+v", got)
	}
	if len(got.Trace) != 1 || got.Trace[0].Kind != SpanTool || got.Trace[0].Name != "orders.lookup" {
		t.Fatalf("unexpected trace: %+v", got.Trace)
	}
	if got.Trace[0].Metadata["cache_hit"] != true {
		t.Fatalf("nested trace metadata did not decode: %+v", got.Trace[0].Metadata)
	}
	if got.Metadata["flow"] != "support.lookup" || got.Metadata["tier"] != "critical" {
		t.Fatalf("unexpected metadata: %+v", got.Metadata)
	}
}

func TestLoadNamedAgentCasesLoadsNamesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_cases.json")
	if err := os.WriteFile(path, []byte(`{
		"agent_cases": [
			{"name": "lookup", "messages": [{"role": "user", "content": "lookup"}]},
			{"name": "refund", "messages": [{"role": "user", "content": "refund"}]}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases, err := LoadNamedAgentCases(path)
	if err != nil {
		t.Fatalf("LoadNamedAgentCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected two cases, got %d", len(cases))
	}
	if cases[0].Name != "lookup" || cases[1].Name != "refund" {
		t.Fatalf("unexpected names: %+v", cases)
	}
}

func TestAgentDatasetEmptyMarshalRoundTrip(t *testing.T) {
	data, err := json.Marshal(AgentDataset{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `{"agent_cases":[]}` {
		t.Fatalf("expected empty agent_cases array, got %s", data)
	}
	if _, err := DecodeAgentDataset(strings.NewReader(string(data))); err != nil {
		t.Fatalf("DecodeAgentDataset: %v", err)
	}
}

func TestDecodeAgentDatasetInvalidFilesReturnClearErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "malformed json",
			payload: `{"agent_cases": [`,
			want:    "decode agent dataset:",
		},
		{
			name:    "missing agent_cases",
			payload: `{}`,
			want:    "agent_cases is required",
		},
		{
			name:    "unknown top-level field",
			payload: `{"cases":[]}`,
			want:    `unknown field "cases"`,
		},
		{
			name:    "null agent_cases",
			payload: `{"agent_cases":null}`,
			want:    "agent_cases must be an array",
		},
		{
			name:    "unknown agent case field",
			payload: `{"agent_cases":[{"name":"lookup","input":"unsupported"}]}`,
			want:    `unknown field "input"`,
		},
		{
			name:    "unknown nested message field",
			payload: `{"agent_cases":[{"name":"lookup","messages":[{"speaker":"user"}]}]}`,
			want:    `unknown field "speaker"`,
		},
		{
			name:    "invalid trace metadata",
			payload: `{"agent_cases":[{"name":"lookup","trace":[{"metadata":[]}]}]}`,
			want:    "cannot unmarshal array",
		},
		{
			name:    "null agent case",
			payload: `{"agent_cases":[null]}`,
			want:    "agent case is null",
		},
		{
			name:    "trailing value",
			payload: `{"agent_cases":[]} {"agent_cases":[]}`,
			want:    "multiple JSON values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeAgentDataset(strings.NewReader(tt.payload))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestDecodeNamedAgentCasesRequiresNames(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "missing",
			payload: `{"agent_cases":[{"messages":[{"role":"user","content":"hi"}]}]}`,
		},
		{
			name:    "whitespace",
			payload: `{"agent_cases":[{"name":"   ","messages":[{"role":"user","content":"hi"}]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeNamedAgentCases(strings.NewReader(tt.payload))
			if err == nil {
				t.Fatalf("expected missing name error")
			}
			if !strings.Contains(err.Error(), "agent case 1: name is required") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAgentDatasetNestedMetadataRoundTrip(t *testing.T) {
	want := AgentDataset{
		Cases: []NamedAgentCase{
			{
				Name: "order-status",
				Case: AgentCase{
					Messages: []Message{{Role: RoleUser, Content: "Where is my order?"}},
					Trace: []TraceSpan{
						{
							Kind:     SpanTool,
							Name:     "orders.lookup",
							Metadata: map[string]any{"cache_hit": true},
						},
					},
					Metadata: map[string]any{"flow": "support.lookup"},
				},
			},
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := DecodeAgentDataset(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("DecodeAgentDataset: %v", err)
	}
	if len(got.Cases) != 1 || len(got.Cases[0].Case.Trace) != 1 {
		t.Fatalf("unexpected cases: %+v", got.Cases)
	}
	if got.Cases[0].Case.Trace[0].Metadata["cache_hit"] != true {
		t.Fatalf("trace metadata did not round-trip: %+v", got.Cases[0].Case.Trace[0].Metadata)
	}
	if got.Cases[0].Case.Metadata["flow"] != "support.lookup" {
		t.Fatalf("case metadata did not round-trip: %+v", got.Cases[0].Case.Metadata)
	}
}
