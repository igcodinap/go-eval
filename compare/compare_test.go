package compare

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestCompareReportsScorePassTokenAndLatencyDeltas(t *testing.T) {
	baseline := []eval.RunResult{
		{
			TestName:         "TestEval/regress",
			Metric:           "Faithfulness",
			Score:            0.9,
			Passed:           true,
			Tokens:           10,
			PromptTokens:     4,
			CompletionTokens: 6,
			LatencyNS:        100,
		},
		{
			TestName:  "TestEval/improve",
			Metric:    "Faithfulness",
			Score:     0.4,
			Passed:    false,
			Tokens:    8,
			LatencyNS: 200,
		},
	}
	current := []eval.RunResult{
		{
			TestName:         "TestEval/regress",
			Metric:           "Faithfulness",
			Score:            0.7,
			Passed:           true,
			Tokens:           15,
			PromptTokens:     5,
			CompletionTokens: 10,
			LatencyNS:        120,
		},
		{
			TestName:  "TestEval/improve",
			Metric:    "Faithfulness",
			Score:     0.8,
			Passed:    true,
			Tokens:    7,
			LatencyNS: 150,
		},
	}

	report := Compare(baseline, current)

	if report.Summary.Total != 2 || report.Summary.Improved != 1 || report.Summary.Regressed != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}

	regressed := findEntry(t, report, "TestEval/regress", "", "Faithfulness")
	if regressed.Status != StatusRegressed {
		t.Fatalf("expected regressed status, got %q", regressed.Status)
	}
	assertFloat(t, regressed.Delta.Score, -0.2)
	if regressed.Delta.Tokens != 5 ||
		regressed.Delta.PromptTokens != 1 ||
		regressed.Delta.CompletionTokens != 4 ||
		regressed.Delta.LatencyNS != 20 {
		t.Fatalf("unexpected regressed delta: %+v", regressed.Delta)
	}
	if regressed.Delta.Pass != PassUnchanged {
		t.Fatalf("unexpected pass delta: %q", regressed.Delta.Pass)
	}

	improved := findEntry(t, report, "TestEval/improve", "", "Faithfulness")
	if improved.Status != StatusImproved {
		t.Fatalf("expected improved status, got %q", improved.Status)
	}
	assertFloat(t, improved.Delta.Score, 0.4)
	if improved.Delta.Pass != PassFailedToPass {
		t.Fatalf("unexpected pass delta: %q", improved.Delta.Pass)
	}
}

func TestCompareRepresentsMissingAndAddedRowsDeterministically(t *testing.T) {
	baseline := []eval.RunResult{
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.8, Passed: true},
		{TestName: "TestEval/c", Metric: "Faithfulness", Score: 0.8, Passed: true},
	}
	current := []eval.RunResult{
		{TestName: "TestEval/b", Metric: "Faithfulness", Score: 0.8, Passed: true},
		{TestName: "TestEval/c", Metric: "Faithfulness", Score: 0.8, Passed: true},
	}

	report := Compare(baseline, current)

	if report.Summary.Total != 3 ||
		report.Summary.Missing != 1 ||
		report.Summary.Added != 1 ||
		report.Summary.Unchanged != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	want := []struct {
		testName string
		status   Status
	}{
		{"TestEval/a", StatusMissing},
		{"TestEval/b", StatusAdded},
		{"TestEval/c", StatusUnchanged},
	}
	if len(report.Entries) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(report.Entries))
	}
	for i, wantEntry := range want {
		got := report.Entries[i]
		if got.Identity.TestName != wantEntry.testName || got.Status != wantEntry.status {
			t.Fatalf("entry %d: got %+v, want test=%q status=%q", i, got, wantEntry.testName, wantEntry.status)
		}
	}
}

func TestCompareWithOptionsUsesCaseIdentity(t *testing.T) {
	baseline := []eval.RunResult{
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.5, Passed: true, Metadata: map[string]any{"case_id": "b"}},
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.8, Passed: true, Metadata: map[string]any{"case_id": "a"}},
	}
	current := []eval.RunResult{
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.7, Passed: true, Metadata: map[string]any{"case_id": "a"}},
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.6, Passed: true, Metadata: map[string]any{"case_id": "b"}},
	}
	identity := func(result eval.RunResult) Identity {
		caseName, _ := result.Metadata["case_id"].(string)
		return Identity{TestName: result.TestName, CaseName: caseName, Metric: result.Metric}
	}

	report := CompareWithOptions(baseline, current, Options{Identity: identity})

	if len(report.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(report.Entries))
	}
	if report.Entries[0].Identity.CaseName != "a" || report.Entries[0].Status != StatusRegressed {
		t.Fatalf("unexpected first entry: %+v", report.Entries[0])
	}
	if report.Entries[1].Identity.CaseName != "b" || report.Entries[1].Status != StatusImproved {
		t.Fatalf("unexpected second entry: %+v", report.Entries[1])
	}
}

func TestReadJSONLRejectsMalformedLine(t *testing.T) {
	_, err := ReadJSONL(strings.NewReader(`{"test_name":"TestEval","metric":"Faithfulness"}` + "\n" + `{bad json}`))
	if err == nil {
		t.Fatalf("expected malformed JSONL error")
	}
	if !strings.Contains(err.Error(), "jsonl line 2") {
		t.Fatalf("expected line number in error, got %v", err)
	}
}

func TestCompareFilesReadsJSONLFiles(t *testing.T) {
	dir := t.TempDir()
	baselinePath := filepath.Join(dir, "baseline.jsonl")
	currentPath := filepath.Join(dir, "current.jsonl")
	if err := os.WriteFile(baselinePath, []byte(`{"test_name":"TestEval","metric":"Faithfulness","score":0.5,"passed":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile baseline: %v", err)
	}
	if err := os.WriteFile(currentPath, []byte(`{"test_name":"TestEval","metric":"Faithfulness","score":0.6,"passed":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}

	report, err := CompareFiles(baselinePath, currentPath)
	if err != nil {
		t.Fatalf("CompareFiles: %v", err)
	}
	entry := findEntry(t, report, "TestEval", "", "Faithfulness")
	if entry.Status != StatusImproved {
		t.Fatalf("expected improved entry, got %+v", entry)
	}
}

func TestCompareIncludesCompoundDimensionDeltas(t *testing.T) {
	baseline := []eval.RunResult{
		{
			TestName: "TestEval/compound",
			Metric:   "Compound",
			Score:    0.8,
			Passed:   true,
			Dimensions: []eval.DimensionResult{
				{Name: "factuality", Score: 0.9, Threshold: 0.7, Passed: true},
				{Name: "removed", Score: 0.8, Threshold: 0.7, Passed: true},
				{Name: "style", Score: 0.7, Threshold: 0.7, Passed: true},
			},
		},
	}
	current := []eval.RunResult{
		{
			TestName: "TestEval/compound",
			Metric:   "Compound",
			Score:    0.8,
			Passed:   true,
			Dimensions: []eval.DimensionResult{
				{Name: "added", Score: 0.9, Threshold: 0.7, Passed: true},
				{Name: "factuality", Score: 0.6, Threshold: 0.75, Passed: false},
				{Name: "style", Score: 0.8, Threshold: 0.7, Passed: true},
			},
		},
	}

	report := Compare(baseline, current)

	entry := findEntry(t, report, "TestEval/compound", "", "Compound")
	if entry.Status != StatusRegressed {
		t.Fatalf("expected dimension regression to mark entry regressed, got %q", entry.Status)
	}

	factuality := findDimension(t, entry, "factuality")
	if factuality.Status != StatusRegressed || factuality.Delta.Pass != PassPassedToFail {
		t.Fatalf("unexpected factuality dimension: %+v", factuality)
	}
	assertFloat(t, factuality.Delta.Score, -0.3)
	assertFloat(t, factuality.Delta.Threshold, 0.05)

	style := findDimension(t, entry, "style")
	if style.Status != StatusImproved {
		t.Fatalf("unexpected style dimension: %+v", style)
	}
	assertFloat(t, style.Delta.Score, 0.1)

	if got := findDimension(t, entry, "added"); got.Status != StatusAdded || !got.HasCurrent {
		t.Fatalf("unexpected added dimension: %+v", got)
	}
	if got := findDimension(t, entry, "removed"); got.Status != StatusMissing || !got.HasBaseline {
		t.Fatalf("unexpected removed dimension: %+v", got)
	}
}

func findEntry(t *testing.T, report Report, testName string, caseName string, metric string) Entry {
	t.Helper()
	for _, entry := range report.Entries {
		if entry.Identity.TestName == testName &&
			entry.Identity.CaseName == caseName &&
			entry.Identity.Metric == metric {
			return entry
		}
	}
	t.Fatalf("entry not found: test=%q case=%q metric=%q", testName, caseName, metric)
	return Entry{}
}

func findDimension(t *testing.T, entry Entry, name string) DimensionEntry {
	t.Helper()
	for _, dimension := range entry.Dimensions {
		if dimension.Name == name {
			return dimension
		}
	}
	t.Fatalf("dimension not found: %q in %+v", name, entry.Dimensions)
	return DimensionEntry{}
}

func assertFloat(t *testing.T, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %f, want %f", got, want)
	}
}
