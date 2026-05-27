package eval

import (
	"context"
	"fmt"
)

// Contract runs several checks as one named metric.
type Contract struct {
	ContractName  string
	Checks        []Metric
	StopOnFailure bool
}

// NewContract returns a Contract with the provided name and checks.
func NewContract(name string, checks ...Metric) Contract {
	return Contract{ContractName: name, Checks: checks}
}

// Name implements Metric.
func (m Contract) Name() string {
	if m.ContractName == "" {
		return "Contract"
	}
	return "Contract(" + m.ContractName + ")"
}

// Score implements Metric.
func (m Contract) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if m.ContractName == "" {
		return Result{Metric: m.Name()}, fmt.Errorf("Contract: name is required")
	}
	if len(m.Checks) == 0 {
		return Result{Metric: m.Name()}, fmt.Errorf("Contract %q: at least one check is required", m.ContractName)
	}

	dimensions := make([]DimensionResult, 0, len(m.Checks))
	passed := true
	scoreSum := 0.0
	for i, check := range m.Checks {
		if check == nil {
			return Result{Metric: m.Name()}, fmt.Errorf("Contract %q: check %d is nil", m.ContractName, i+1)
		}
		result, err := check.Score(ctx, j, c)
		if err != nil {
			return Result{Metric: m.Name()}, fmt.Errorf("Contract %q: check %s: %w", m.ContractName, check.Name(), err)
		}
		if result.Metric == "" {
			result.Metric = check.Name()
		}
		dimensions = append(dimensions, DimensionResult{
			Name:   result.Metric,
			Score:  result.Score,
			Passed: result.Passed,
			Reason: result.Reason,
		})
		scoreSum += result.Score
		if !result.Passed {
			passed = false
			if m.StopOnFailure {
				break
			}
		}
	}

	score := scoreSum / float64(len(dimensions))
	reason := "all checks passed"
	if !passed {
		reason = contractFailureReason(dimensions)
	}
	return Result{
		Score:      score,
		Passed:     passed,
		Metric:     m.Name(),
		Reason:     reason,
		Dimensions: dimensions,
	}, nil
}

func contractFailureReason(dimensions []DimensionResult) string {
	for _, dimension := range dimensions {
		if !dimension.Passed {
			if dimension.Reason == "" {
				return fmt.Sprintf("check %q failed", dimension.Name)
			}
			return fmt.Sprintf("check %q failed: %s", dimension.Name, dimension.Reason)
		}
	}
	return "one or more checks failed"
}
