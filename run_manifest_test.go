package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRunManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunManifestFileName)
	manifest := NewRunManifest()
	manifest.GoEvalVersion = "v1.1.0-test"
	manifest.RunID = "run-123"
	manifest.RunName = "release smoke"
	manifest.Repo = "github.com/igcodinap/go-eval"
	manifest.Branch = "main"
	manifest.Commit = "abcdef123"
	manifest.ExitCode = 1
	manifest.Status = "failed"
	manifest.Command = []string{"go", "test", "./..."}
	manifest.Profile = "release"
	manifest.Packages = []string{"./...", "./cmd/goeval"}
	manifest.ResultsPath = "results.jsonl"
	manifest.TracesPath = "traces.jsonl"
	manifest.JudgeEventsPath = "judge-events.jsonl"
	manifest.TestEventsPath = "test-events.jsonl"
	manifest.SummaryPath = "summary.json"
	manifest.ReportPath = "report.html"
	manifest.StartedAt = "2026-07-05T12:00:00Z"
	manifest.EndedAt = "2026-07-05T12:00:01Z"
	manifest.DurationNS = 1_000_000_000
	manifest.Metadata = map[string]any{"gate": "release"}

	if err := WriteRunManifest(path, manifest); err != nil {
		t.Fatalf("WriteRunManifest: %v", err)
	}

	got, err := ReadRunManifest(path)
	if err != nil {
		t.Fatalf("ReadRunManifest: %v", err)
	}
	if got.SchemaVersion != 1 || got.ResultsSchemaVersion != ResultsSchemaVersion || got.TraceSchemaVersion != TraceSchemaVersion {
		t.Fatalf("schema versions = %+v", got)
	}
	if got.GoEvalVersion != "v1.1.0-test" ||
		got.RunID != "run-123" ||
		got.RunName != "release smoke" ||
		got.Repo != "github.com/igcodinap/go-eval" ||
		got.Branch != "main" ||
		got.Commit != "abcdef123" ||
		got.ExitCode != 1 ||
		got.Status != "failed" ||
		got.Command[2] != "./..." ||
		got.Profile != "release" ||
		got.Packages[1] != "./cmd/goeval" ||
		got.ResultsPath != "results.jsonl" ||
		got.TracesPath != "traces.jsonl" ||
		got.JudgeEventsPath != "judge-events.jsonl" ||
		got.TestEventsPath != "test-events.jsonl" ||
		got.SummaryPath != "summary.json" ||
		got.ReportPath != "report.html" ||
		got.DurationNS != 1_000_000_000 ||
		got.Metadata["gate"] != "release" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
}

func TestReadRunManifestPreV12Compatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunManifestFileName)
	if err := os.WriteFile(path, []byte(`{
		"schema_version": 1,
		"goeval_version": "v1.1.0",
		"results_schema_version": 1,
		"trace_schema_version": 1,
		"command": ["go", "test", "./..."],
		"results_path": "results.jsonl",
		"traces_path": "traces.jsonl",
		"judge_events_path": "judge-events.jsonl"
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadRunManifest(path)
	if err != nil {
		t.Fatalf("ReadRunManifest: %v", err)
	}
	if got.RunID != "" || got.Status != "" || got.TestEventsPath != "" || got.SummaryPath != "" || got.ReportPath != "" {
		t.Fatalf("new fields should decode to zero values: %+v", got)
	}
	if got.ResultsPath != "results.jsonl" || got.TracesPath != "traces.jsonl" || got.JudgeEventsPath != "judge-events.jsonl" {
		t.Fatalf("old fields not preserved: %+v", got)
	}
}

func TestWriteRunManifestFillsSchemaDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunManifestFileName)
	if err := WriteRunManifest(path, RunManifest{}); err != nil {
		t.Fatalf("WriteRunManifest: %v", err)
	}
	got, err := ReadRunManifest(path)
	if err != nil {
		t.Fatalf("ReadRunManifest: %v", err)
	}
	if got.SchemaVersion != 1 || got.ResultsSchemaVersion != ResultsSchemaVersion || got.TraceSchemaVersion != TraceSchemaVersion {
		t.Fatalf("schema defaults were not filled: %+v", got)
	}
}
