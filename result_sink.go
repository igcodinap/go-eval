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

const runResultKindScenarioSummary = "scenario_summary"

// RunResult is the serialized form of a metric run.
type RunResult struct {
	Timestamp        string            `json:"timestamp"`
	Kind             string            `json:"kind,omitempty"`
	TestName         string            `json:"test_name"`
	Metric           string            `json:"metric"`
	ScenarioName     string            `json:"scenario_name,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
	Score            float64           `json:"score"`
	Passed           bool              `json:"passed"`
	Reason           string            `json:"reason"`
	Tokens           int               `json:"tokens"`
	PromptTokens     int               `json:"prompt_tokens,omitempty"`
	CompletionTokens int               `json:"completion_tokens,omitempty"`
	LatencyNS        int64             `json:"latency_ns"`
	Dimensions       []DimensionResult `json:"dimensions,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	ScenarioSummary  *ScenarioSummary  `json:"scenario_summary,omitempty"`
}

// ScenarioSummary is the JSONL diagnostic summary emitted after RunScenario.
type ScenarioSummary struct {
	Name         string            `json:"name"`
	Passed       bool              `json:"passed"`
	RunCount     int               `json:"run_count"`
	PassRuns     int               `json:"pass_runs"`
	TraceIDs     []string          `json:"trace_ids,omitempty"`
	Steps        []StepSummary     `json:"steps,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	ArtifactKeys []string          `json:"artifact_keys,omitempty"`
	Dimensions   []DimensionResult `json:"dimensions,omitempty"`
}

// StepSummary captures per-step diagnostics for scenario JSONL summaries.
type StepSummary struct {
	Name          string         `json:"name"`
	Passed        bool           `json:"passed"`
	RepeatRun     int            `json:"repeat_run,omitempty"`
	ToolCalls     []string       `json:"tool_calls,omitempty"`
	ArtifactKeys  []string       `json:"artifact_keys,omitempty"`
	FailedMetrics []string       `json:"failed_metrics,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
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
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
		TestName:         tbName,
		Metric:           result.Metric,
		TraceID:          result.TraceID,
		Score:            result.Score,
		Passed:           result.Passed,
		Reason:           result.Reason,
		Tokens:           result.Tokens,
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
		LatencyNS:        int64(result.Latency),
		Dimensions:       result.Dimensions,
		Metadata:         result.Metadata,
	}
}
