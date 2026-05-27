package eval

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
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

// OutputLengthBudget fails when an agent output is longer than configured.
//
// MaxRunes and MaxWords are both optional. When both are set, both budgets must
// pass. Counts are deterministic approximations over Case.Output, not model
// tokenizer counts.
type OutputLengthBudget struct {
	MaxRunes int
	MaxWords int
}

// Name implements Metric.
func (m OutputLengthBudget) Name() string { return "OutputLengthBudget" }

// Score implements Metric.
func (m OutputLengthBudget) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	runes := len([]rune(c.Output))
	words := wordCount(c.Output)
	if m.MaxRunes <= 0 && m.MaxWords <= 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: "output length budget disabled",
		}, nil
	}

	var failures []string
	score := 1.0
	if m.MaxRunes > 0 && runes > m.MaxRunes {
		failures = append(failures, fmt.Sprintf("runes %d > %d", runes, m.MaxRunes))
		score = minFloat(score, safeRatio(m.MaxRunes, runes))
	}
	if m.MaxWords > 0 && words > m.MaxWords {
		failures = append(failures, fmt.Sprintf("words %d > %d", words, m.MaxWords))
		score = minFloat(score, safeRatio(m.MaxWords, words))
	}
	if len(failures) == 0 {
		return Result{
			Score:  1,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("output length within budget: %d runes, %d words", runes, words),
		}, nil
	}
	return Result{
		Score:  score,
		Passed: false,
		Metric: m.Name(),
		Reason: "output length budget exceeded: " + strings.Join(failures, "; "),
	}, nil
}

func appendBudgetReason(reason string, budgetReason string) string {
	if reason == "" {
		return budgetReason
	}
	return budgetReason + "; " + reason
}

func wordCount(s string) int {
	inWord := false
	count := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			count++
			inWord = true
		}
	}
	return count
}

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func metricName(metric Metric) string {
	if metric == nil {
		return "<nil>"
	}
	return metric.Name()
}
