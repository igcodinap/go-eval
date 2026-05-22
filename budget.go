package eval

import (
	"context"
	"fmt"
	"time"
)

// TokenBudget fails the wrapped metric when its result exceeds MaxTokens.
//
// When the budget is exceeded, the inner metric's score and token counts are
// preserved and Passed is set to false.
type TokenBudget struct {
	Metric    Metric
	MaxTokens int
}

// WithTokenBudget wraps a metric with a token budget.
//
// A maxTokens value <= 0 disables the budget check.
func WithTokenBudget(maxTokens int, metric Metric) Metric {
	return TokenBudget{Metric: metric, MaxTokens: maxTokens}
}

// Name implements Metric.
func (m TokenBudget) Name() string {
	return "TokenBudget(" + metricName(m.Metric) + ")"
}

// Score implements Metric.
func (m TokenBudget) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if m.Metric == nil {
		return Result{Metric: m.Name()}, fmt.Errorf("TokenBudget: metric is nil")
	}

	result, err := m.Metric.Score(ctx, j, c)
	if err != nil {
		return result, err
	}
	if m.MaxTokens <= 0 || result.Tokens <= m.MaxTokens {
		return result, nil
	}

	result.Passed = false
	result.Reason = appendBudgetReason(
		result.Reason,
		fmt.Sprintf("token budget exceeded: %d > %d", result.Tokens, m.MaxTokens),
	)
	return result, nil
}

// LatencyBudget fails the wrapped metric when its result exceeds MaxLatency.
//
// When the budget is exceeded, the inner metric's score and latency are
// preserved and Passed is set to false. If the inner metric leaves Latency at
// zero, the wrapper fills it with wall-clock execution time.
type LatencyBudget struct {
	Metric     Metric
	MaxLatency time.Duration
}

// WithLatencyBudget wraps a metric with a latency budget.
//
// A maxLatency value <= 0 disables the budget check.
func WithLatencyBudget(maxLatency time.Duration, metric Metric) Metric {
	return LatencyBudget{Metric: metric, MaxLatency: maxLatency}
}

// Name implements Metric.
func (m LatencyBudget) Name() string {
	return "LatencyBudget(" + metricName(m.Metric) + ")"
}

// Score implements Metric.
func (m LatencyBudget) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if m.Metric == nil {
		return Result{Metric: m.Name()}, fmt.Errorf("LatencyBudget: metric is nil")
	}

	start := time.Now()
	result, err := m.Metric.Score(ctx, j, c)
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}
	if err != nil {
		return result, err
	}
	if m.MaxLatency <= 0 || result.Latency <= m.MaxLatency {
		return result, nil
	}

	result.Passed = false
	result.Reason = appendBudgetReason(
		result.Reason,
		fmt.Sprintf("latency budget exceeded: %s > %s", result.Latency, m.MaxLatency),
	)
	return result, nil
}

func appendBudgetReason(reason string, budgetReason string) string {
	if reason == "" {
		return budgetReason
	}
	return budgetReason + "; " + reason
}

func metricName(metric Metric) string {
	if metric == nil {
		return "<nil>"
	}
	return metric.Name()
}
