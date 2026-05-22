package eval

import (
	"encoding/json"
	"testing"
)

func TestTurnToolCallJSONRoundTrip(t *testing.T) {
	want := Turn{
		Role:       RoleAssistant,
		Content:    "Checking the route.",
		Name:       "assistant",
		ToolCallID: "call-1",
		ToolCalls: []ToolCall{
			{
				ID:        "call-1",
				Name:      "route.lookup",
				Arguments: json.RawMessage(`{"from":"Santiago","to":"Valparaiso"}`),
				Result:    "ready",
				Metadata:  map[string]any{"source": "planner"},
			},
		},
		Metadata: map[string]any{"flow": "route.plan"},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Turn
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Role != want.Role || got.Content != want.Content || got.ToolCallID != want.ToolCallID {
		t.Fatalf("unexpected turn: %+v", got)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", got.ToolCalls)
	}
	call := got.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "route.lookup" || call.Result != "ready" {
		t.Fatalf("unexpected tool call: %+v", call)
	}
	if string(call.Arguments) != `{"from":"Santiago","to":"Valparaiso"}` {
		t.Fatalf("unexpected arguments: %s", call.Arguments)
	}
}

func TestCaseTrajectoryFieldsAreOptional(t *testing.T) {
	c := Case{Input: "q", Output: "a"}
	if c.Turns != nil {
		t.Fatalf("expected nil turns by default")
	}
	if c.ExpectedToolCalls != nil {
		t.Fatalf("expected nil expected tool calls by default")
	}
}
