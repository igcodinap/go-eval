package eval

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// EnvVar gates eval execution. When unset or empty, Runner.Run and Bench skip.
const EnvVar = "GOEVAL"

// Runner holds shared state and executes metrics against cases.
//
// Runner is safe for concurrent use so one instance can be shared across
// parallel subtests and benchmarks.
type Runner struct {
	judge      Judge
	timeout    time.Duration
	sink       ResultSink
	caseFilter func(Case) bool
	sinkMu     sync.Mutex
}

// Option configures a Runner at construction time.
type Option func(*Runner)

// WithTimeout sets a per-metric timeout. The default is 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) {
		r.timeout = d
	}
}

// WithCaseFilter skips cases for which pred returns false.
//
// A nil predicate leaves Runner behavior unchanged.
func WithCaseFilter(pred func(Case) bool) Option {
	return func(r *Runner) {
		r.caseFilter = pred
	}
}

// NewRunner returns a Runner bound to the provided Judge.
func NewRunner(j Judge, opts ...Option) *Runner {
	r := &Runner{
		judge:   j,
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes one metric against one case and asserts via tb.
//
// If GOEVAL is unset or the case filter excludes the case, the evaluation is
// skipped. Metric errors are fatal.
// Low scores are test errors but do not stop the test. The resulting Result
// is returned in all cases so callers can chain their own assertions.
func (r *Runner) Run(tb testing.TB, m Metric, c Case) Result {
	tb.Helper()

	if os.Getenv(EnvVar) == "" {
		tb.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return Result{}
	}
	if r.caseFilter != nil && !r.caseFilter(c) {
		tb.Skip("eval skipped by case filter")
		return Result{}
	}

	return r.runResult(tb, m.Name(), func(ctx context.Context, j Judge) (Result, error) {
		return m.Score(ctx, j, c)
	}, c.Metadata)
}

// RunAgent executes one agent metric against one agent case and asserts via tb.
//
// It mirrors Run while scoring an AgentCase with an AgentMetric. Case filters
// apply to the AgentCase metadata through the same Case view used by Run.
func (r *Runner) RunAgent(tb testing.TB, m AgentMetric, c AgentCase) Result {
	tb.Helper()

	if os.Getenv(EnvVar) == "" {
		tb.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return Result{}
	}
	if r.caseFilter != nil && !r.caseFilter(c.caseView()) {
		tb.Skip("eval skipped by case filter")
		return Result{}
	}

	return r.runResult(tb, m.Name(), func(ctx context.Context, j Judge) (Result, error) {
		return m.ScoreAgent(ctx, j, c)
	}, c.Metadata)
}

func (r *Runner) runResult(
	tb testing.TB,
	metricName string,
	score func(context.Context, Judge) (Result, error),
	metadata map[string]any,
) Result {
	ctx, cancel := runnerContext(r.timeout)
	defer cancel()

	judge := maybeTrace(r.judge, tb)

	start := time.Now()
	result, err := score(ctx, judge)
	if result.Metadata == nil && len(metadata) > 0 {
		result.Metadata = copyMetadata(metadata)
	}
	if result.Metric == "" {
		result.Metric = metricName
	}
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}

	if err != nil {
		tb.Fatalf("%s: judge error: %v", metricName, err)
		return result
	}

	if !result.Passed {
		tb.Errorf("%s=%.2f below threshold\nReason: %s", result.Metric, result.Score, result.Reason)
		r.writeResult(tb, result)
		return result
	}

	tb.Logf("%s=%.2f pass (reason: %s)", result.Metric, result.Score, result.Reason)
	r.writeResult(tb, result)
	return result
}

func copyMetadata(metadata map[string]any) map[string]any {
	out := make(map[string]any, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out
}

func (r *Runner) writeResult(tb testing.TB, result Result) {
	if r.sink == nil {
		return
	}

	r.sinkMu.Lock()
	err := r.sink.Write(newRunResult(tb.Name(), result))
	r.sinkMu.Unlock()
	if err != nil {
		tb.Errorf("result sink: %v", err)
	}
}

func runnerContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}
