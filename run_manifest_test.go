package eval

import (
	"path/filepath"
	"testing"
)

func TestWriteReadRunManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), RunManifestFileName)
	manifest := NewRunManifest()
	manifest.GoEvalVersion = "v1.1.0-test"
	manifest.Command = []string{"go", "test", "./..."}
	manifest.Profile = "release"
	manifest.Packages = []string{"./...", "./cmd/goeval"}
	manifest.ResultsPath = "results.jsonl"
	manifest.TracesPath = "traces.jsonl"
	manifest.JudgeEventsPath = "judge-events.jsonl"
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
		got.Command[2] != "./..." ||
		got.Profile != "release" ||
		got.Packages[1] != "./cmd/goeval" ||
		got.ResultsPath != "results.jsonl" ||
		got.TracesPath != "traces.jsonl" ||
		got.JudgeEventsPath != "judge-events.jsonl" ||
		got.DurationNS != 1_000_000_000 ||
		got.Metadata["gate"] != "release" {
		t.Fatalf("unexpected manifest: %+v", got)
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
