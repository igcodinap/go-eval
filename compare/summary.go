package compare

import (
	"math"
	"time"

	eval "github.com/igcodinap/go-eval"
)

// ResultsSummary aggregates one JSONL result file.
type ResultsSummary struct {
	Total    int
	Passed   int
	Failed   int
	ByMetric map[string]MetricSummary
}

// MetricSummary aggregates result rows for one metric.
type MetricSummary struct {
	Count       int
	Passed      int
	Failed      int
	MeanScore   float64
	StdDev      float64
	MinScore    float64
	MaxScore    float64
	MeanLatency time.Duration
	MeanTokens  float64
}

type metricAccumulator struct {
	count        int
	passed       int
	scoreSum     float64
	scoreSquares float64
	minScore     float64
	maxScore     float64
	latencySum   int64
	tokenSum     int
}

// SummarizeFile reads and summarizes one JSONL result file.
func SummarizeFile(path string) (ResultsSummary, error) {
	results, err := ReadJSONLFile(path)
	if err != nil {
		return ResultsSummary{}, err
	}
	return Summarize(results), nil
}

// Summarize aggregates one set of result rows by pass/fail and metric.
func Summarize(results []eval.RunResult) ResultsSummary {
	summary := ResultsSummary{
		ByMetric: map[string]MetricSummary{},
	}
	accumulators := map[string]*metricAccumulator{}

	for _, result := range results {
		summary.Total++
		if result.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}

		acc := accumulators[result.Metric]
		if acc == nil {
			acc = &metricAccumulator{
				minScore: math.Inf(1),
				maxScore: math.Inf(-1),
			}
			accumulators[result.Metric] = acc
		}
		acc.add(result)
	}

	for metric, acc := range accumulators {
		summary.ByMetric[metric] = acc.summary()
	}
	return summary
}

func (a *metricAccumulator) add(result eval.RunResult) {
	a.count++
	if result.Passed {
		a.passed++
	}
	a.scoreSum += result.Score
	a.scoreSquares += result.Score * result.Score
	a.minScore = math.Min(a.minScore, result.Score)
	a.maxScore = math.Max(a.maxScore, result.Score)
	a.latencySum += result.LatencyNS
	a.tokenSum += result.Tokens
}

func (a metricAccumulator) summary() MetricSummary {
	mean := a.scoreSum / float64(a.count)
	variance := a.scoreSquares/float64(a.count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return MetricSummary{
		Count:       a.count,
		Passed:      a.passed,
		Failed:      a.count - a.passed,
		MeanScore:   mean,
		StdDev:      math.Sqrt(variance),
		MinScore:    a.minScore,
		MaxScore:    a.maxScore,
		MeanLatency: time.Duration(a.latencySum / int64(a.count)),
		MeanTokens:  float64(a.tokenSum) / float64(a.count),
	}
}
