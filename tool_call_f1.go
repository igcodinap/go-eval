package eval

import (
	"context"
	"fmt"
)

// ToolCallF1 reports precision, recall, and F1 for actual tool calls.
type ToolCallF1 struct {
	MatchArgs   bool
	MatchResult bool
	Threshold   float64
}

// Name implements Metric.
func (m ToolCallF1) Name() string { return "ToolCallF1" }

// Score implements Metric.
func (m ToolCallF1) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	actual := flattenToolCalls(c.Turns)
	expected := c.ExpectedToolCalls
	matched, err := countToolCallMatches(actual, expected, toolCallMatchOptions{
		matchArgs:   m.MatchArgs,
		matchResult: m.MatchResult,
	})
	if err != nil {
		return failedTrajectoryResult(m.Name(), err.Error()), nil
	}

	precision := safeRatio(matched, len(actual))
	recall := safeRatio(matched, len(expected))
	f1 := 0.0
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}

	threshold := defaultFloat(m.Threshold, 0.8)
	passed := f1 >= threshold
	return Result{
		Score:  f1,
		Passed: passed,
		Metric: m.Name(),
		Reason: fmt.Sprintf("tool call precision %.2f, recall %.2f, f1 %.2f", precision, recall, f1),
		Dimensions: []DimensionResult{
			{Name: "precision", Score: precision, Threshold: 0, Passed: true},
			{Name: "recall", Score: recall, Threshold: 0, Passed: true},
			{Name: "f1", Score: f1, Threshold: threshold, Passed: passed},
		},
	}, nil
}
