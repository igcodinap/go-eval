package eval

import (
	"context"
	"fmt"
)

// RequiredTools fails when any configured tool name is absent from Case.Turns.
type RequiredTools struct {
	Names []string
}

// Name implements Metric.
func (m RequiredTools) Name() string { return "RequiredTools" }

// Score implements Metric.
func (m RequiredTools) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if len(m.Names) == 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "no required tool names configured",
		}, nil
	}

	seen := make(map[string]struct{})
	for _, call := range flattenToolCalls(c.Turns) {
		seen[call.Name] = struct{}{}
	}

	for _, name := range m.Names {
		if _, ok := seen[name]; !ok {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("required tool not used: %s", name),
			}, nil
		}
	}

	return Result{
		Score:  1,
		Passed: true,
		Metric: m.Name(),
		Reason: "all required tools used",
	}, nil
}
