package eval

import (
	"context"
	"time"
)

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

const (
	// SpanTool identifies a tool call in an agent trace.
	SpanTool = "tool"
	// SpanRetrieval identifies a retrieval step in an agent trace.
	SpanRetrieval = "retrieval"
	// SpanLLM identifies an LLM call in an agent trace.
	SpanLLM = "llm"
)

// Message is one conversational turn in an agent evaluation case.
type Message struct {
	Role       string         `json:"role,omitempty"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// TraceSpan captures one relevant step in an agent run.
type TraceSpan struct {
	Kind     string         `json:"kind,omitempty"`
	Name     string         `json:"name,omitempty"`
	Input    string         `json:"input,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Context  []string       `json:"context,omitempty"`
	Latency  time.Duration  `json:"latency,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentCase is a multi-turn, trace-aware evaluation input.
type AgentCase struct {
	Messages []Message      `json:"messages,omitempty"`
	Output   string         `json:"output,omitempty"`
	Expected string         `json:"expected,omitempty"`
	Context  []string       `json:"context,omitempty"`
	Trace    []TraceSpan    `json:"trace,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentMetric is the contract for trace-aware agent evaluations.
type AgentMetric interface {
	Name() string
	ScoreAgent(ctx context.Context, j Judge, c AgentCase) (Result, error)
}

func (c AgentCase) caseView() Case {
	return Case{
		Output:   c.Output,
		Expected: c.Expected,
		Context:  c.Context,
		Metadata: c.Metadata,
	}
}
