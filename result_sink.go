package eval

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ResultsDirEnvVar is the env var used by DefaultResultSink.
const ResultsDirEnvVar = "GOEVAL_RESULTS_DIR"

// RunResult is the serialized form of a metric run.
type RunResult struct {
	Timestamp  string            `json:"timestamp"`
	TestName   string            `json:"test_name"`
	Metric     string            `json:"metric"`
	Score      float64           `json:"score"`
	Passed     bool              `json:"passed"`
	Reason     string            `json:"reason"`
	Tokens     int               `json:"tokens"`
	LatencyNS  int64             `json:"latency_ns"`
	Dimensions []DimensionResult `json:"dimensions,omitempty"`
}

// ResultSink receives per-run serialized results.
//
// Implementations should be safe for concurrent use.
type ResultSink interface {
	Write(RunResult) error
}

// WithResultSink configures a Runner to write RunResult values.
func WithResultSink(sink ResultSink) Option {
	return func(r *Runner) {
		r.sink = sink
	}
}

// DefaultResultSink creates a JSONL sink from GOEVAL_RESULTS_DIR.
//
// Returns nil when the env var is unset.
func DefaultResultSink() ResultSink {
	dir := os.Getenv(ResultsDirEnvVar)
	if dir == "" {
		return nil
	}
	return &jsonlFileSink{
		path: filepath.Join(dir, "results.jsonl"),
	}
}

type jsonlFileSink struct {
	path string
	mu   sync.Mutex
}

func (s *jsonlFileSink) Write(result RunResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	writeErr := enc.Encode(result)
	closeErr := f.Close()
	if writeErr != nil && closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func newRunResult(tbName string, result Result) RunResult {
	return RunResult{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		TestName:   tbName,
		Metric:     result.Metric,
		Score:      result.Score,
		Passed:     result.Passed,
		Reason:     result.Reason,
		Tokens:     result.Tokens,
		LatencyNS:  int64(result.Latency),
		Dimensions: result.Dimensions,
	}
}
