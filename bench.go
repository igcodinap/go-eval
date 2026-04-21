package eval

import (
	"math"
	"os"
	"testing"
)

// Bench runs a metric b.N times against the same Case and reports
// LLM-specific benchmark metrics via b.ReportMetric.
//
// Reported values:
//   - tokens/op: mean tokens consumed per judge call
//   - score_mean: mean score across iterations
//   - score_stddev: population standard deviation of scores
//
// Like Runner.Run, Bench is gated by GOEVAL.
func Bench(b *testing.B, r *Runner, m Metric, c Case) {
	b.Helper()

	if os.Getenv(EnvVar) == "" {
		b.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return
	}

	var totalTokens int
	scores := make([]float64, 0, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := runnerContext(r.timeout)
		result, err := m.Score(ctx, r.judge, c)
		cancel()
		if err != nil {
			b.Fatalf("%s: judge error: %v", m.Name(), err)
			return
		}
		totalTokens += result.Tokens
		scores = append(scores, result.Score)
	}
	b.StopTimer()

	if b.N == 0 {
		return
	}

	b.ReportMetric(float64(totalTokens)/float64(b.N), "tokens/op")

	var sum float64
	for _, score := range scores {
		sum += score
	}
	mean := sum / float64(b.N)

	var sumSq float64
	for _, score := range scores {
		delta := score - mean
		sumSq += delta * delta
	}
	stddev := math.Sqrt(sumSq / float64(b.N))

	b.ReportMetric(mean, "score_mean")
	b.ReportMetric(stddev, "score_stddev")
}
