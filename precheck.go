package eval

import (
	"context"
	"errors"
	"fmt"
)

// Precheck composes two metrics and short-circuits Main when Pre fails.
type Precheck struct {
	Pre  Metric
	Main Metric
}

// Name implements Metric.
func (m Precheck) Name() string {
	pre := "<nil>"
	main := "<nil>"
	if m.Pre != nil {
		pre = m.Pre.Name()
	}
	if m.Main != nil {
		main = m.Main.Name()
	}
	return "Precheck(" + pre + "->" + main + ")"
}

// Score implements Metric.
func (m Precheck) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if m.Pre == nil {
		return Result{Metric: m.Name()}, errors.New("precheck: Pre metric is nil")
	}
	if m.Main == nil {
		return Result{Metric: m.Name()}, errors.New("precheck: Main metric is nil")
	}

	preResult, err := m.Pre.Score(ctx, j, c)
	if err != nil {
		return Result{Metric: m.Main.Name()}, fmt.Errorf("precheck: pre metric %s: %w", m.Pre.Name(), err)
	}

	if !preResult.Passed {
		return Result{
			Score:   0,
			Passed:  false,
			Metric:  m.Main.Name(),
			Reason:  fmt.Sprintf("precheck<%s> failed: %s", m.Pre.Name(), preResult.Reason),
			Latency: preResult.Latency,
			Tokens:  preResult.Tokens,
		}, nil
	}

	mainResult, err := m.Main.Score(ctx, j, c)
	if err != nil {
		return mainResult, fmt.Errorf("precheck: main metric %s: %w", m.Main.Name(), err)
	}

	if mainResult.Metric == "" {
		mainResult.Metric = m.Main.Name()
	}
	mainResult.Tokens += preResult.Tokens
	mainResult.Latency += preResult.Latency
	return mainResult, nil
}
