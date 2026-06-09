package eval

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// EnvVar gates eval execution. When unset or empty, Runner.Run and Bench skip.
const EnvVar = "GOEVAL"

// TierEnvVar filters eval execution when DefaultTierFilter is installed.
const TierEnvVar = "GOEVAL_TIER"

// Runner holds shared state and executes metrics against cases.
//
// Runner is safe for concurrent use so one instance can be shared across
// parallel subtests and benchmarks.
type Runner struct {
	judge      Judge
	timeout    time.Duration
	sink       ResultSink
	traceSink  TraceSink
	redactors  []Redactor
	caseFilter func(Case) bool
	tierFilter []string
	traceSeen  map[string]struct{}
	sinkMu     sync.Mutex
	traceMu    sync.Mutex
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

// WithTierFilter skips cases whose metadata tier is not in tiers.
//
// Empty tier strings are ignored. If every tier is empty, the option is a no-op.
func WithTierFilter(tiers ...string) Option {
	return func(r *Runner) {
		r.tierFilter = append(r.tierFilter, cleanTiers(tiers)...)
	}
}

// DefaultTierFilter returns a tier filter option using GOEVAL_TIER.
//
// Runners do not read GOEVAL_TIER unless this option is installed.
func DefaultTierFilter() Option {
	return WithTierFilter(splitTierEnv(os.Getenv(TierEnvVar))...)
}

// NewRunner returns a Runner bound to the provided Judge.
func NewRunner(j Judge, opts ...Option) *Runner {
	r := &Runner{
		judge:     j,
		timeout:   30 * time.Second,
		traceSeen: map[string]struct{}{},
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
	if !r.shouldRun(c) {
		tb.Skip("eval skipped by case filter")
		return Result{}
	}

	ctx, cancel := runnerContext(r.timeoutForCase(c))
	defer cancel()

	judge := maybeTrace(r.judge, tb)

	start := time.Now()
	result, err := m.Score(ctx, judge, c)
	if result.Metadata == nil && len(c.Metadata) > 0 {
		metadata := make(map[string]any, len(c.Metadata))
		for k, v := range c.Metadata {
			metadata[k] = v
		}
		result.Metadata = metadata
	}
	if result.Metric == "" {
		result.Metric = m.Name()
	}
	r.attachTrace(tb, tb.Name(), c, &result)
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
	r.writeResultNamed(tb, tb.Name(), result)
}

func (r *Runner) writeResultNamed(tb testing.TB, testName string, result Result) {
	r.writeRunResult(tb, newRunResult(testName, result))
}

func (r *Runner) writeRunResult(tb testing.TB, runResult RunResult) {
	if r.sink == nil {
		return
	}

	runResult = r.redactRunResult(runResult)

	r.sinkMu.Lock()
	err := r.sink.Write(runResult)
	r.sinkMu.Unlock()
	if err != nil {
		tb.Errorf("result sink: %v", err)
	}
}

func (r *Runner) attachTrace(tb testing.TB, testName string, c Case, result *Result) {
	tb.Helper()
	if result == nil {
		return
	}
	traceID := c.TraceID
	if c.Trace != nil {
		trace := cloneTrace(c.Trace)
		if trace.ID == "" && traceID != "" {
			trace.ID = traceID
		}
		traceID = ensureTraceID(&trace)
		if trace.TestName == "" {
			trace.TestName = testName
		}
		if trace.Name == "" {
			trace.Name = testName
		}
		r.writeTrace(tb, trace)
	}
	if result.TraceID == "" {
		result.TraceID = traceID
	}
}

func (r *Runner) writeTrace(tb testing.TB, trace Trace) {
	tb.Helper()
	if r.traceSink == nil {
		return
	}
	if trace.ID != "" {
		r.traceMu.Lock()
		if _, ok := r.traceSeen[trace.ID]; ok {
			r.traceMu.Unlock()
			return
		}
		r.traceSeen[trace.ID] = struct{}{}
		r.traceMu.Unlock()
	}

	trace = r.redactTrace(trace)

	r.traceMu.Lock()
	err := r.traceSink.WriteTrace(trace)
	r.traceMu.Unlock()
	if err != nil {
		tb.Errorf("trace sink: %v", err)
	}
}

func (r *Runner) shouldRun(c Case) bool {
	if len(r.tierFilter) > 0 && !tierMatches(c.Metadata, r.tierFilter) {
		return false
	}
	return r.caseFilter == nil || r.caseFilter(c)
}

func (r *Runner) timeoutForCase(c Case) time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return r.timeout
}

func runnerContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

func tierMatches(metadata map[string]any, tiers []string) bool {
	tier, ok := metadata["tier"].(string)
	if !ok {
		return false
	}
	for _, want := range tiers {
		if tier == want {
			return true
		}
	}
	return false
}

func splitTierEnv(value string) []string {
	if value == "" {
		return nil
	}
	return cleanTiers(strings.Split(value, ","))
}

func cleanTiers(tiers []string) []string {
	out := make([]string, 0, len(tiers))
	seen := map[string]struct{}{}
	for _, tier := range tiers {
		tier = strings.TrimSpace(tier)
		if tier == "" {
			continue
		}
		if _, exists := seen[tier]; exists {
			continue
		}
		seen[tier] = struct{}{}
		out = append(out, tier)
	}
	return out
}
