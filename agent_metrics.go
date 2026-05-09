package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/task_completion.tmpl
var taskCompletionTmpl string

//go:embed prompts/tool_use_correctness.tmpl
var toolUseCorrectnessTmpl string

//go:embed prompts/agent_geval.tmpl
var agentGEvalTmpl string

var taskCompletionTemplate = template.Must(template.New("task_completion").Parse(taskCompletionTmpl))
var toolUseCorrectnessTemplate = template.Must(template.New("tool_use_correctness").Parse(toolUseCorrectnessTmpl))
var agentGEvalTemplate = template.Must(template.New("agent_geval").Funcs(gevalFuncs).Parse(agentGEvalTmpl))

// TaskCompletion measures whether an agent's final output satisfies the user
// goal and expected outcome.
type TaskCompletion struct {
	Threshold float64
}

// Name implements AgentMetric.
func (m TaskCompletion) Name() string { return "TaskCompletion" }

// ScoreAgent implements AgentMetric.
func (m TaskCompletion) ScoreAgent(ctx context.Context, j Judge, c AgentCase) (Result, error) {
	return runTemplateMetric(ctx, j, taskCompletionTemplate, c, "task_completion", m.Name(), defaultFloat(m.Threshold, 0.8))
}

// ToolUseCorrectness measures whether an agent's tool and retrieval steps were
// necessary, correctly sequenced, and consistent with the final answer.
type ToolUseCorrectness struct {
	Threshold float64
}

// Name implements AgentMetric.
func (m ToolUseCorrectness) Name() string { return "ToolUseCorrectness" }

// ScoreAgent implements AgentMetric.
func (m ToolUseCorrectness) ScoreAgent(ctx context.Context, j Judge, c AgentCase) (Result, error) {
	return runTemplateMetric(ctx, j, toolUseCorrectnessTemplate, c, "tool_use_correctness", m.Name(), defaultFloat(m.Threshold, 0.8))
}

// AgentGEval is a custom trace-aware LLM-as-judge metric driven by a
// user-supplied rubric.
type AgentGEval struct {
	Criteria  string
	Steps     []string
	Threshold float64
}

// Name implements AgentMetric.
func (m AgentGEval) Name() string { return "AgentGEval" }

// ScoreAgent implements AgentMetric.
func (m AgentGEval) ScoreAgent(ctx context.Context, j Judge, c AgentCase) (Result, error) {
	data := struct {
		AgentCase
		Criteria string
		Steps    []string
	}{
		AgentCase: c,
		Criteria:  m.Criteria,
		Steps:     m.Steps,
	}

	return runTemplateMetric(ctx, j, agentGEvalTemplate, data, "agent_geval", m.Name(), defaultFloat(m.Threshold, 0.7))
}
