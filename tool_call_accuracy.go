package eval

import (
	"context"
	"fmt"
)

// ToolCallAccuracy compares actual tool calls in Case.Turns with Case.ExpectedToolCalls.
type ToolCallAccuracy struct {
	Mode        MatchMode
	MatchArgs   bool
	MatchResult bool
	Threshold   float64
}

// Name implements Metric.
func (m ToolCallAccuracy) Name() string { return "ToolCallAccuracy" }

// Score implements Metric.
func (m ToolCallAccuracy) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	mode, err := normalizeMatchMode(m.Mode)
	if err != nil {
		return failedTrajectoryResult(m.Name(), err.Error()), nil
	}

	actual := flattenToolCalls(c.Turns)
	expected := c.ExpectedToolCalls
	score, err := toolCallAccuracyScore(actual, expected, mode, toolCallMatchOptions{
		matchArgs:   m.MatchArgs,
		matchResult: m.MatchResult,
	})
	if err != nil {
		return failedTrajectoryResult(m.Name(), err.Error()), nil
	}

	threshold := defaultFloat(m.Threshold, 1)
	return Result{
		Score:  score,
		Passed: score >= threshold,
		Metric: m.Name(),
		Reason: fmt.Sprintf("tool call accuracy %.2f with %s matching", score, mode),
	}, nil
}

func toolCallAccuracyScore(actual []ToolCall, expected []ToolCall, mode MatchMode, opts toolCallMatchOptions) (float64, error) {
	switch mode {
	case MatchStrict:
		return strictToolCallScore(actual, expected, opts)
	case MatchUnordered:
		matched, err := countToolCallMatches(actual, expected, opts)
		return safeRatio(matched, max(len(actual), len(expected))), err
	case MatchSubset:
		matched, err := countToolCallMatches(actual, expected, opts)
		return safeRatio(matched, len(expected)), err
	case MatchSuperset:
		matched, err := countToolCallMatches(actual, expected, opts)
		return safeRatio(matched, len(actual)), err
	default:
		return 0, fmt.Errorf("unsupported match mode %q", mode)
	}
}

func strictToolCallScore(actual []ToolCall, expected []ToolCall, opts toolCallMatchOptions) (float64, error) {
	denominator := max(len(actual), len(expected))
	if denominator == 0 {
		return 1, nil
	}

	matched := 0
	for i := 0; i < min(len(actual), len(expected)); i++ {
		ok, err := toolCallsMatch(actual[i], expected[i], opts)
		if err != nil {
			return 0, err
		}
		if ok {
			matched++
		}
	}
	return float64(matched) / float64(denominator), nil
}

func failedTrajectoryResult(metricName string, reason string) Result {
	return Result{
		Score:  0,
		Passed: false,
		Metric: metricName,
		Reason: reason,
	}
}
