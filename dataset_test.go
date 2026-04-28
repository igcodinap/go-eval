package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeCases_LoadsJSONDataset(t *testing.T) {
	cases, err := DecodeCases(strings.NewReader(`{
		"cases": [
			{
				"name": "france-capital",
				"input": "What is the capital of France?",
				"output": "Paris is the capital of France.",
				"expected": "Paris",
				"context": ["Paris is the capital of France."],
				"metadata": {
					"flow": "rag.answer",
					"tier": "critical",
					"dataset": "capitals/smoke-v1"
				}
			}
		]
	}`))
	if err != nil {
		t.Fatalf("DecodeCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one case, got %d", len(cases))
	}

	got := cases[0]
	if got.Input != "What is the capital of France?" ||
		got.Output != "Paris is the capital of France." ||
		got.Expected != "Paris" {
		t.Fatalf("unexpected case strings: %+v", got)
	}
	if len(got.Context) != 1 || got.Context[0] != "Paris is the capital of France." {
		t.Fatalf("unexpected context: %+v", got.Context)
	}
	if got.Metadata["flow"] != "rag.answer" ||
		got.Metadata["tier"] != "critical" ||
		got.Metadata["dataset"] != "capitals/smoke-v1" {
		t.Fatalf("unexpected metadata: %+v", got.Metadata)
	}
}

func TestLoadNamedCases_LoadsNamesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(path, []byte(`{
		"cases": [
			{"name": "paris", "input": "What is the capital of France?"},
			{"name": "rome", "input": "What is the capital of Italy?"}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases, err := LoadNamedCases(path)
	if err != nil {
		t.Fatalf("LoadNamedCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected two cases, got %d", len(cases))
	}
	if cases[0].Name != "paris" || cases[0].Case.Input != "What is the capital of France?" {
		t.Fatalf("unexpected first case: %+v", cases[0])
	}
	if cases[1].Name != "rome" || cases[1].Case.Input != "What is the capital of Italy?" {
		t.Fatalf("unexpected second case: %+v", cases[1])
	}
}

func TestDecodeCases_EmptyDataset(t *testing.T) {
	cases, err := DecodeCases(strings.NewReader(`{"cases":[]}`))
	if err != nil {
		t.Fatalf("DecodeCases: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("expected no cases, got %d", len(cases))
	}
	if cases == nil {
		t.Fatalf("expected empty non-nil cases slice")
	}
}

func TestDataset_EmptyMarshalRoundTrip(t *testing.T) {
	data, err := json.Marshal(Dataset{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `{"cases":[]}` {
		t.Fatalf("expected empty cases array, got %s", data)
	}
	if _, err := DecodeDataset(strings.NewReader(string(data))); err != nil {
		t.Fatalf("DecodeDataset: %v", err)
	}
}

func TestDecodeDataset_InvalidFilesReturnClearErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "malformed json",
			payload: `{"cases": [`,
			want:    "decode dataset:",
		},
		{
			name:    "missing cases",
			payload: `{}`,
			want:    "cases is required",
		},
		{
			name:    "unknown top-level field",
			payload: `{"casez":[]}`,
			want:    `unknown field "casez"`,
		},
		{
			name:    "null cases",
			payload: `{"cases":null}`,
			want:    "cases must be an array",
		},
		{
			name:    "unknown case field",
			payload: `{"cases":[{"name":"france","question":"What is the capital of France?"}]}`,
			want:    `unknown field "question"`,
		},
		{
			name:    "invalid metadata",
			payload: `{"cases":[{"name":"france","metadata":[]}]}`,
			want:    "cannot unmarshal array",
		},
		{
			name:    "null case",
			payload: `{"cases":[null]}`,
			want:    "case is null",
		},
		{
			name:    "trailing value",
			payload: `{"cases":[]} {"cases":[]}`,
			want:    "multiple JSON values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeDataset(strings.NewReader(tt.payload))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestDecodeNamedCases_RequiresNames(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "missing",
			payload: `{"cases":[{"input":"What is the capital of France?"}]}`,
		},
		{
			name:    "whitespace",
			payload: `{"cases":[{"name":"   ","input":"What is the capital of France?"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeNamedCases(strings.NewReader(tt.payload))
			if err == nil {
				t.Fatalf("expected missing name error")
			}
			if !strings.Contains(err.Error(), "case 1: name is required") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDataset_MetadataRoundTrip(t *testing.T) {
	want := Dataset{
		Cases: []NamedCase{
			{
				Name: "france-capital",
				Case: Case{
					Input:    "What is the capital of France?",
					Expected: "Paris",
					Metadata: map[string]any{
						"flow":    "rag.answer",
						"tier":    "critical",
						"dataset": "capitals/smoke-v1",
					},
				},
			},
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := DecodeDataset(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("DecodeDataset: %v", err)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("expected one case, got %d", len(got.Cases))
	}
	metadata := got.Cases[0].Case.Metadata
	if metadata["flow"] != "rag.answer" ||
		metadata["tier"] != "critical" ||
		metadata["dataset"] != "capitals/smoke-v1" {
		t.Fatalf("metadata did not round-trip: %+v", metadata)
	}
}
