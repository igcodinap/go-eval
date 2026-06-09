package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

var taskCompletionTemplate = template.Must(template.New("task_completion").Parse(`You are grading whether an agent completed the user's task.

Score from 0 to 1.
Return JSON with fields "score" and "reason".

User input:
{{.Input}}

Expected outcome:
{{.Expected}}

Agent output:
{{.Output}}

Agent trace:
{{.Trace}}

Rubric:
{{.Rubric}}
`))

var planAdherenceTemplate = template.Must(template.New("plan_adherence").Parse(`You are grading whether an agent followed the expected plan.

Score from 0 to 1.
Return JSON with fields "score" and "reason".

Expected plan:
{{.Plan}}

Agent output:
{{.Output}}

Agent trace:
{{.Trace}}

Rubric:
{{.Rubric}}
`))

// TaskCompletion judges whether the agent completed the user's task.
type TaskCompletion struct {
	Threshold float64
	Rubric    string
}

// Name implements Metric.
func (m TaskCompletion) Name() string { return "TaskCompletion" }

// Score implements Metric.
func (m TaskCompletion) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	rubric := strings.TrimSpace(m.Rubric)
	if rubric == "" {
		rubric = "The output satisfies the user request and any expected outcome without requiring unresolved follow-up."
	}
	return runTemplateMetric(ctx, j, taskCompletionTemplate, struct {
		Input    string
		Expected string
		Output   string
		Trace    string
		Rubric   string
	}{
		Input:    c.Input,
		Expected: c.Expected,
		Output:   c.Output,
		Trace:    caseTraceText(c),
		Rubric:   rubric,
	}, "task_completion", m.Name(), defaultFloat(m.Threshold, 0.8))
}

// ToolArgumentAccuracy scores expected tool names and arguments against the
// observed trace or turn tool calls.
type ToolArgumentAccuracy struct {
	Threshold float64
}

// Name implements Metric.
func (m ToolArgumentAccuracy) Name() string { return "ToolArgumentAccuracy" }

// Score implements Metric.
func (m ToolArgumentAccuracy) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	expected := c.ExpectedToolCalls
	actual := toolCallsFromCase(c)
	if len(expected) == 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "no expected tool arguments configured",
		}, nil
	}

	matched, err := countToolCallMatches(actual, expected, toolCallMatchOptions{matchArgs: true})
	if err != nil {
		return failedTrajectoryResult(m.Name(), err.Error()), nil
	}
	score := safeRatio(matched, len(expected))
	threshold := defaultFloat(m.Threshold, 1)
	return Result{
		Score:  score,
		Passed: score >= threshold,
		Metric: m.Name(),
		Reason: fmt.Sprintf("matched %d/%d expected tool arguments", matched, len(expected)),
	}, nil
}

// PlanAdherence judges whether the agent followed an expected plan. The plan is
// read from Case.Expected, or from metadata key "plan" when Expected is empty.
type PlanAdherence struct {
	Threshold float64
	Rubric    string
}

// Name implements Metric.
func (m PlanAdherence) Name() string { return "PlanAdherence" }

// Score implements Metric.
func (m PlanAdherence) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	plan := planFromCase(c)
	if strings.TrimSpace(plan) == "" {
		return Result{
			Score:  0,
			Passed: false,
			Metric: m.Name(),
			Reason: "expected plan is empty",
		}, nil
	}
	rubric := strings.TrimSpace(m.Rubric)
	if rubric == "" {
		rubric = "The trace follows the important steps in the expected plan without skipping required steps or taking contradictory actions."
	}
	return runTemplateMetric(ctx, j, planAdherenceTemplate, struct {
		Plan   string
		Output string
		Trace  string
		Rubric string
	}{
		Plan:   plan,
		Output: c.Output,
		Trace:  caseTraceText(c),
		Rubric: rubric,
	}, "plan_adherence", m.Name(), defaultFloat(m.Threshold, 0.7))
}

// StepEfficiency checks whether the observed trace stays within configured step
// and tool-call budgets.
type StepEfficiency struct {
	MaxSteps     int
	MaxToolCalls int
	Threshold    float64
}

// Name implements Metric.
func (m StepEfficiency) Name() string { return "StepEfficiency" }

// Score implements Metric.
func (m StepEfficiency) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	maxToolCalls := m.MaxToolCalls
	if maxToolCalls <= 0 && len(c.ExpectedToolCalls) > 0 {
		maxToolCalls = len(c.ExpectedToolCalls)
	}
	if m.MaxSteps <= 0 && maxToolCalls <= 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "no step or tool-call budget configured",
		}, nil
	}

	stepCount := stepCountFromCase(c)
	toolCallCount := len(toolCallsFromCase(c))
	var scores []float64
	var parts []string
	if m.MaxSteps > 0 {
		scores = append(scores, budgetScore(stepCount, m.MaxSteps))
		parts = append(parts, fmt.Sprintf("steps=%d/%d", stepCount, m.MaxSteps))
	}
	if maxToolCalls > 0 {
		scores = append(scores, budgetScore(toolCallCount, maxToolCalls))
		parts = append(parts, fmt.Sprintf("tool_calls=%d/%d", toolCallCount, maxToolCalls))
	}

	score := meanFloat64(scores)
	threshold := defaultFloat(m.Threshold, 1)
	return Result{
		Score:  score,
		Passed: score >= threshold,
		Metric: m.Name(),
		Reason: "efficiency budget " + strings.Join(parts, " "),
	}, nil
}

func toolCallsFromCase(c Case) []ToolCall {
	if c.Trace != nil {
		var calls []ToolCall
		for _, span := range c.Trace.Spans {
			if span.ToolCall != nil {
				calls = append(calls, *span.ToolCall)
			}
		}
		if len(calls) > 0 {
			return calls
		}
	}
	return flattenToolCalls(c.Turns)
}

func stepCountFromCase(c Case) int {
	if c.Trace != nil {
		count := 0
		for _, span := range c.Trace.Spans {
			if span.Kind != "tool_call" {
				count++
			}
		}
		if count > 0 {
			return count
		}
	}
	return len(c.Turns)
}

func budgetScore(actual int, budget int) float64 {
	if budget <= 0 || actual <= budget {
		return 1
	}
	if actual <= 0 {
		return 1
	}
	return float64(budget) / float64(actual)
}

func meanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 1
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func planFromCase(c Case) string {
	if strings.TrimSpace(c.Expected) != "" {
		return c.Expected
	}
	if value, ok := c.Metadata["plan"]; ok && value != nil {
		switch typed := value.(type) {
		case string:
			return typed
		case []string:
			return strings.Join(typed, "\n")
		default:
			data, err := json.Marshal(typed)
			if err == nil {
				return string(data)
			}
		}
	}
	return ""
}

func caseTraceText(c Case) string {
	if c.Trace != nil {
		data, err := json.MarshalIndent(c.Trace, "", "  ")
		if err == nil {
			return string(data)
		}
	}
	if len(c.Turns) > 0 {
		data, err := json.MarshalIndent(c.Turns, "", "  ")
		if err == nil {
			return string(data)
		}
	}
	return "(no trace)"
}
