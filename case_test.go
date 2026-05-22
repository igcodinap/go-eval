package eval

import (
	"encoding/json"
	"testing"
)

func TestCase_FieldsAccessibleViaStructLiteral(t *testing.T) {
	c := Case{
		Input:    "q",
		Output:   "a",
		Expected: "e",
		Context:  []string{"doc1", "doc2"},
		Turns: []Turn{
			{
				Role:    RoleUser,
				Content: "q",
			},
		},
		ExpectedToolCalls: []ToolCall{
			{
				Name:      "search",
				Arguments: json.RawMessage(`{"query":"q"}`),
			},
		},
		Metadata: map[string]any{"trace_id": "abc"},
		Artifacts: map[string]json.RawMessage{
			"state": json.RawMessage(`{"status":"ready"}`),
		},
	}

	if c.Input != "q" || c.Output != "a" || c.Expected != "e" {
		t.Fatalf("unexpected string fields: %+v", c)
	}
	if len(c.Context) != 2 || c.Context[0] != "doc1" {
		t.Fatalf("unexpected Context: %+v", c.Context)
	}
	if len(c.Turns) != 1 || c.Turns[0].Role != RoleUser {
		t.Fatalf("unexpected Turns: %+v", c.Turns)
	}
	if len(c.ExpectedToolCalls) != 1 || c.ExpectedToolCalls[0].Name != "search" {
		t.Fatalf("unexpected ExpectedToolCalls: %+v", c.ExpectedToolCalls)
	}
	if c.Metadata["trace_id"] != "abc" {
		t.Fatalf("unexpected Metadata: %+v", c.Metadata)
	}
	if string(c.Artifacts["state"]) != `{"status":"ready"}` {
		t.Fatalf("unexpected Artifacts: %+v", c.Artifacts)
	}
}
