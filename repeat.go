package eval

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Repeat runs a metric multiple times and aggregates pass rate and score stats.
type Repeat struct {
	Metric   Metric
	N        int
	PassRate float64
}

// RepeatN returns a Repeat wrapper that requires every run to pass.
func RepeatN(n int, metric Metric) Metric {
	return Repeat{Metric: metric, N: n}
}

// Name implements Metric.
func (m Repeat) Name() string {
	return "Repeat(" + metricName(m.Metric) + ")"
}

// Score implements Metric.
func (m Repeat) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if m.Metric == nil {
		return Result{Metric: m.Name()}, fmt.Errorf("Repeat: metric is nil")
	}
	if m.N < 2 {
		return Result{Metric: m.Name()}, fmt.Errorf("Repeat: N must be >= 2, got %d", m.N)
	}

	requiredPassRate := defaultFloat(m.PassRate, 1)
	if requiredPassRate < 0 || requiredPassRate > 1 {
		return Result{Metric: m.Name()}, fmt.Errorf("Repeat: PassRate must be between 0 and 1, got %g", m.PassRate)
	}

	stats := repeatStats{
		minScore: math.Inf(1),
		maxScore: math.Inf(-1),
	}
	for i := 0; i < m.N; i++ {
		result, err := m.Metric.Score(ctx, j, c)
		if err != nil {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("run %d failed before repeat aggregation: %v", i+1, err),
			}, fmt.Errorf("Repeat: run %d: %w", i+1, err)
		}
		stats.add(result)
	}
	return stats.result(m.Name(), requiredPassRate), nil
}

type repeatStats struct {
	count            int
	passed           int
	scoreSum         float64
	scoreSumSquares  float64
	minScore         float64
	maxScore         float64
	tokens           int
	promptTokens     int
	completionTokens int
	latency          int64
}

func (s *repeatStats) add(result Result) {
	s.count++
	if result.Passed {
		s.passed++
	}
	s.scoreSum += result.Score
	s.scoreSumSquares += result.Score * result.Score
	s.minScore = math.Min(s.minScore, result.Score)
	s.maxScore = math.Max(s.maxScore, result.Score)
	s.tokens += result.Tokens
	s.promptTokens += result.PromptTokens
	s.completionTokens += result.CompletionTokens
	s.latency += int64(result.Latency)
}

func (s repeatStats) result(metricName string, requiredPassRate float64) Result {
	mean := s.scoreSum / float64(s.count)
	variance := s.scoreSumSquares/float64(s.count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)
	passRate := float64(s.passed) / float64(s.count)
	passed := passRate >= requiredPassRate

	return Result{
		Score:            mean,
		Passed:           passed,
		Metric:           metricName,
		Reason:           fmt.Sprintf("%d/%d runs passed, mean score %.2f", s.passed, s.count, mean),
		Tokens:           s.tokens,
		PromptTokens:     s.promptTokens,
		CompletionTokens: s.completionTokens,
		Latency:          time.Duration(s.latency),
		Dimensions: []DimensionResult{
			{Name: "pass_rate", Score: passRate, Threshold: requiredPassRate, Passed: passed},
			{Name: "mean_score", Score: mean, Threshold: 0, Passed: true},
			{Name: "stddev", Score: stddev, Threshold: 0, Passed: true},
			{Name: "min_score", Score: s.minScore, Threshold: 0, Passed: true},
			{Name: "max_score", Score: s.maxScore, Threshold: 0, Passed: true},
		},
	}
}
