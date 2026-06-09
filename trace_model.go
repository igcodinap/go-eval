package eval

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Trace captures a structured agent or LLM trajectory.
type Trace struct {
	ID           string           `json:"id"`
	Name         string           `json:"name,omitempty"`
	TestName     string           `json:"test_name,omitempty"`
	ScenarioName string           `json:"scenario_name,omitempty"`
	StartedAt    string           `json:"started_at,omitempty"`
	EndedAt      string           `json:"ended_at,omitempty"`
	DurationNS   int64            `json:"duration_ns,omitempty"`
	Spans        []Span           `json:"spans,omitempty"`
	Artifacts    []ArtifactRecord `json:"artifacts,omitempty"`
	StateDeltas  []StateDelta     `json:"state_deltas,omitempty"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
}

// Span captures one operation inside a Trace.
type Span struct {
	ID         string         `json:"id,omitempty"`
	ParentID   string         `json:"parent_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	StartedAt  string         `json:"started_at,omitempty"`
	EndedAt    string         `json:"ended_at,omitempty"`
	DurationNS int64          `json:"duration_ns,omitempty"`
	Input      string         `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	Error      string         `json:"error,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ArtifactRecord captures a named artifact observed during a trace.
type ArtifactRecord struct {
	Key      string          `json:"key,omitempty"`
	Name     string          `json:"name,omitempty"`
	MIMEType string          `json:"mime_type,omitempty"`
	URI      string          `json:"uri,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// StateDelta captures one structured state transition observed during a trace.
type StateDelta struct {
	Key      string          `json:"key,omitempty"`
	Before   json.RawMessage `json:"before,omitempty"`
	After    json.RawMessage `json:"after,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// TraceSink receives structured traces.
//
// Implementations should be safe for concurrent use.
type TraceSink interface {
	WriteTrace(Trace) error
}

// WithTraceSink configures a Runner to write Trace values.
func WithTraceSink(sink TraceSink) Option {
	return func(r *Runner) {
		r.traceSink = sink
	}
}

// DefaultTraceSink creates a JSONL trace sink from GOEVAL_RESULTS_DIR.
//
// Returns nil when the env var is unset.
func DefaultTraceSink() TraceSink {
	dir := os.Getenv(ResultsDirEnvVar)
	if dir == "" {
		return nil
	}
	return &jsonlTraceSink{
		path: filepath.Join(dir, "traces.jsonl"),
	}
}

type jsonlTraceSink struct {
	path string
	mu   sync.Mutex
}

func (s *jsonlTraceSink) WriteTrace(trace Trace) error {
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
	writeErr := enc.Encode(trace)
	closeErr := f.Close()
	if writeErr != nil && closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func ensureTraceID(trace *Trace) string {
	if trace == nil {
		return ""
	}
	if trace.ID == "" {
		trace.ID = newTraceID()
	}
	return trace.ID
}

func newTraceID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return time.Now().UTC().Format("20060102T150405.000000000")
}
