package eval

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	// RunManifestFileName is the default sidecar file written in GOEVAL_RESULTS_DIR.
	RunManifestFileName = "goeval-run.json"

	// ResultsSchemaVersion is the current results.jsonl schema version.
	ResultsSchemaVersion = 1

	// TraceSchemaVersion is the current traces.jsonl schema version.
	TraceSchemaVersion = 1
)

// RunManifest is an optional sidecar describing one eval run.
//
// It intentionally lives outside results.jsonl so existing result readers stay
// backward compatible.
type RunManifest struct {
	SchemaVersion        int            `json:"schema_version"`
	GoEvalVersion        string         `json:"goeval_version,omitempty"`
	ResultsSchemaVersion int            `json:"results_schema_version"`
	TraceSchemaVersion   int            `json:"trace_schema_version"`
	RunID                string         `json:"run_id,omitempty"`
	RunName              string         `json:"run_name,omitempty"`
	Repo                 string         `json:"repo,omitempty"`
	Branch               string         `json:"branch,omitempty"`
	Commit               string         `json:"commit,omitempty"`
	ExitCode             int            `json:"exit_code,omitempty"`
	Status               string         `json:"status,omitempty"`
	Command              []string       `json:"command,omitempty"`
	Profile              string         `json:"profile,omitempty"`
	Packages             []string       `json:"packages,omitempty"`
	ResultsPath          string         `json:"results_path,omitempty"`
	TracesPath           string         `json:"traces_path,omitempty"`
	JudgeEventsPath      string         `json:"judge_events_path,omitempty"`
	TestEventsPath       string         `json:"test_events_path,omitempty"`
	SummaryPath          string         `json:"summary_path,omitempty"`
	ReportPath           string         `json:"report_path,omitempty"`
	StartedAt            string         `json:"started_at,omitempty"`
	EndedAt              string         `json:"ended_at,omitempty"`
	DurationNS           int64          `json:"duration_ns,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	_                    struct{}
}

// NewRunManifest returns a manifest initialized with current schema versions.
func NewRunManifest() RunManifest {
	return RunManifest{
		SchemaVersion:        1,
		ResultsSchemaVersion: ResultsSchemaVersion,
		TraceSchemaVersion:   TraceSchemaVersion,
	}
}

// DefaultRunManifestPath returns the manifest path for GOEVAL_RESULTS_DIR.
//
// It returns an empty string when GOEVAL_RESULTS_DIR is unset.
func DefaultRunManifestPath() string {
	dir := os.Getenv(ResultsDirEnvVar)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, RunManifestFileName)
}

// WriteRunManifest writes a manifest as indented JSON.
func WriteRunManifest(path string, manifest RunManifest) error {
	if path == "" {
		return errors.New("run manifest path is empty")
	}
	if manifest.SchemaVersion == 0 {
		manifest.SchemaVersion = 1
	}
	if manifest.ResultsSchemaVersion == 0 {
		manifest.ResultsSchemaVersion = ResultsSchemaVersion
	}
	if manifest.TraceSchemaVersion == 0 {
		manifest.TraceSchemaVersion = TraceSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o644)
}

// ReadRunManifest reads a run manifest JSON file.
func ReadRunManifest(path string) (RunManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunManifest{}, err
	}
	var manifest RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return RunManifest{}, err
	}
	return manifest, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
