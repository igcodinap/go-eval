package eval

import (
	"context"
	"fmt"
)

// ForbiddenTool fails when any configured tool name or pattern appears in Case.Turns.
type ForbiddenTool struct {
	Names    []string
	Patterns []string
	Except   []string
}

// Name implements Metric.
func (m ForbiddenTool) Name() string { return "ForbiddenTool" }

// Score implements Metric.
func (m ForbiddenTool) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if len(m.Names) == 0 && len(m.Patterns) == 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "no forbidden tool names configured",
		}, nil
	}

	forbidden := make(map[string]struct{}, len(m.Names))
	for _, name := range m.Names {
		forbidden[name] = struct{}{}
	}

	for _, call := range flattenToolCalls(c.Turns) {
		if _, ok := forbidden[call.Name]; ok {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("forbidden tool used: %s", call.Name),
			}, nil
		}
		matched, pattern, err := toolNameMatchesAnyPattern(call.Name, m.Patterns)
		if err != nil {
			return Result{ //nolint:nilerr // Invalid patterns are represented as failed metric results.
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: err.Error(),
			}, nil
		}
		if matched && !toolNameIn(call.Name, m.Except) {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("forbidden tool pattern %q matched: %s", pattern, call.Name),
			}, nil
		}
	}

	return Result{
		Score:  1,
		Passed: true,
		Metric: m.Name(),
		Reason: "no forbidden tools used",
	}, nil
}
