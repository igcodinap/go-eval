package eval

import (
	"context"
	"fmt"
)

// RequiredTools fails when any configured tool name or pattern is absent from Case.Turns.
type RequiredTools struct {
	Names    []string
	Patterns []string
}

// Name implements Metric.
func (m RequiredTools) Name() string { return "RequiredTools" }

// Score implements Metric.
func (m RequiredTools) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if len(m.Names) == 0 && len(m.Patterns) == 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "no required tool names configured",
		}, nil
	}

	seen := make(map[string]struct{})
	var calls []string
	for _, call := range flattenToolCalls(c.Turns) {
		seen[call.Name] = struct{}{}
		calls = append(calls, call.Name)
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
	for _, pattern := range m.Patterns {
		matched := false
		for _, name := range calls {
			ok, err := toolNameMatchesPattern(name, pattern)
			if err != nil {
				return Result{ //nolint:nilerr // Invalid patterns are represented as failed metric results.
					Score:  0,
					Passed: false,
					Metric: m.Name(),
					Reason: err.Error(),
				}, nil
			}
			if ok {
				matched = true
				break
			}
		}
		if !matched {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("required tool pattern not used: %s", pattern),
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
