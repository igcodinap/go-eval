package eval

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// EvaluatorOption configures an Evaluator.
type EvaluatorOption func(*Evaluator)

// Evaluator executes metrics outside testing.TB.
//
// Runner remains the go test integration. Evaluator is for post-hoc and
// programmatic workflows that need Result values and optional JSONL sinks
// without calling testing.TB methods.
type Evaluator struct {
	judge     Judge
	timeout   time.Duration
	sink      ResultSink
	traceSink TraceSink
	redactors []Redactor
	traceSeen map[string]struct{}
	traceMu   sync.Mutex
}

// NewEvaluator returns an Evaluator bound to the provided Judge.
func NewEvaluator(j Judge, opts ...EvaluatorOption) *Evaluator {
	e := &Evaluator{
		judge:     j,
		timeout:   30 * time.Second,
		traceSeen: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithEvaluatorTimeout sets a per-metric timeout. The default is 30 seconds.
func WithEvaluatorTimeout(d time.Duration) EvaluatorOption {
	return func(e *Evaluator) {
		e.timeout = d
	}
}

// WithEvaluatorResultSink configures a ResultSink for post-hoc runs.
func WithEvaluatorResultSink(sink ResultSink) EvaluatorOption {
	return func(e *Evaluator) {
		e.sink = sink
	}
}

// WithEvaluatorTraceSink configures a TraceSink for post-hoc runs.
func WithEvaluatorTraceSink(sink TraceSink) EvaluatorOption {
	return func(e *Evaluator) {
		e.traceSink = sink
	}
}

// WithEvaluatorRedactors configures redactors for evaluator sink writes.
func WithEvaluatorRedactors(redactors ...Redactor) EvaluatorOption {
	return func(e *Evaluator) {
		for _, redactor := range redactors {
			if redactor != nil {
				e.redactors = append(e.redactors, redactor)
			}
		}
	}
}

// Evaluate executes one metric against one case without testing.TB.
func (e *Evaluator) Evaluate(ctx context.Context, m Metric, c Case) (Result, error) {
	return e.EvaluateNamed(ctx, "", m, c)
}

// EvaluateNamed executes one metric and records testName in optional sinks.
func (e *Evaluator) EvaluateNamed(ctx context.Context, testName string, m Metric, c Case) (Result, error) {
	if e == nil {
		return Result{}, fmt.Errorf("evaluator is nil")
	}
	if m == nil {
		return Result{}, fmt.Errorf("metric is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := e.timeoutForCase(c)
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		cancel = func() {}
	}
	defer cancel()

	start := time.Now()
	result, err := m.Score(ctx, e.judge, c)
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
	if err := e.attachTrace(testName, c, &result); err != nil {
		return result, err
	}
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}
	if err != nil {
		return result, err
	}
	if e.sink != nil {
		runResult := newRunResult(testName, result)
		runResult = e.redactRunResult(runResult)
		if writeErr := e.sink.Write(runResult); writeErr != nil {
			return result, fmt.Errorf("result sink: %w", writeErr)
		}
	}
	return result, nil
}

func (e *Evaluator) timeoutForCase(c Case) time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return e.timeout
}

func (e *Evaluator) attachTrace(testName string, c Case, result *Result) error {
	if result == nil {
		return nil
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
		if err := e.writeTrace(trace); err != nil {
			return fmt.Errorf("trace sink: %w", err)
		}
	}
	if result.TraceID == "" {
		result.TraceID = traceID
	}
	return nil
}

func (e *Evaluator) writeTrace(trace Trace) error {
	if e.traceSink == nil {
		return nil
	}
	trace = e.redactTrace(trace)

	e.traceMu.Lock()
	defer e.traceMu.Unlock()
	if trace.ID != "" {
		if _, ok := e.traceSeen[trace.ID]; ok {
			return nil
		}
	}
	if err := e.traceSink.WriteTrace(trace); err != nil {
		return err
	}
	if trace.ID != "" {
		e.traceSeen[trace.ID] = struct{}{}
	}
	return nil
}

func (e *Evaluator) redactRunResult(result RunResult) RunResult {
	r := &Runner{redactors: e.redactors}
	return r.redactRunResult(result)
}

func (e *Evaluator) redactTrace(trace Trace) Trace {
	r := &Runner{redactors: e.redactors}
	return r.redactTrace(trace)
}
