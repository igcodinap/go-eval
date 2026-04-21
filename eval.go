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
	judge   Judge
	timeout time.Duration
	sink    ResultSink
	sinkMu  sync.Mutex
}

// Option configures a Runner at construction time.
type Option func(*Runner)

// WithTimeout sets a per-metric timeout. The default is 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) {
		r.timeout = d
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
// If GOEVAL is unset, the evaluation is skipped. Metric errors are fatal.
// Low scores are test errors but do not stop the test. The resulting Result
// is returned in all cases so callers can chain their own assertions.
func (r *Runner) Run(tb testing.TB, m Metric, c Case) Result {
	tb.Helper()

	if os.Getenv(EnvVar) == "" {
		tb.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return Result{}
	}

	ctx, cancel := runnerContext(r.timeout)
	defer cancel()

	start := time.Now()
	result, err := m.Score(ctx, r.judge, c)
	if result.Metric == "" {
		result.Metric = m.Name()
	}
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}

	if err != nil {
		tb.Fatalf("%s: judge error: %v", m.Name(), err)
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
