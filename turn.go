package eval

import "encoding/json"

const (
	// RoleSystem identifies an instruction or policy message.
	RoleSystem = "system"
	// RoleUser identifies an end-user message.
	RoleUser = "user"
	// RoleAssistant identifies an assistant message.
	RoleAssistant = "assistant"
	// RoleTool identifies a tool result message.
	RoleTool = "tool"
)

// Turn is one message-like step in a conversation or agent trajectory.
//
// Role values are intentionally open-ended. The Role* constants are conventions
// for common transcripts, not validation gates.
type Turn struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// Name preserves provider-specific speaker or tool labels when present.
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ToolCall describes one tool invocation and its observed or expected result.
type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    string          `json:"result,omitempty"`
	// Error preserves a failed invocation result for reporting or custom metrics.
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
