package eval

import (
	"context"
	"fmt"
)

// StepBudget fails when the flattened tool-call count exceeds MaxSteps.
type StepBudget struct {
	MaxSteps int
}

// Name implements Metric.
func (m StepBudget) Name() string { return "StepBudget" }

// Score implements Metric.
func (m StepBudget) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	steps := len(flattenToolCalls(c.Turns))
	if m.MaxSteps <= 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "step budget disabled",
		}, nil
	}
	if steps <= m.MaxSteps {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("step budget satisfied: %d <= %d", steps, m.MaxSteps),
		}, nil
	}

	return Result{
		Score:  safeRatio(m.MaxSteps, steps),
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("step budget exceeded: %d > %d", steps, m.MaxSteps),
	}, nil
}
