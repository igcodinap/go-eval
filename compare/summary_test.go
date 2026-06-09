package compare

import (
	"os"
	"strings"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestSummarizeAggregatesByMetric(t *testing.T) {
	summary := Summarize([]eval.RunResult{
		{Metric: "Faithfulness", Score: 1.0, Passed: true, Tokens: 10, LatencyNS: 100, Metadata: map[string]any{"tier": "critical", "flow": "rag.answer", "dataset": "smoke/v1", "case_id": "a"}},
		{Metric: "Faithfulness", Score: 0.5, Passed: false, Tokens: 30, LatencyNS: 300, Metadata: map[string]any{"tier": "critical", "flow": "rag.answer", "dataset": "smoke/v1", "case_id": "b"}},
		{Metric: "ToolCallF1", Score: 0.75, Passed: true, Tokens: 0, LatencyNS: 0},
	})

	if summary.Total != 3 || summary.Passed != 2 || summary.Failed != 1 || summary.PassRate != 2.0/3.0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	faithfulness := summary.ByMetric["Faithfulness"]
	if faithfulness.Count != 2 || faithfulness.Passed != 1 || faithfulness.Failed != 1 {
		t.Fatalf("unexpected metric summary: %+v", faithfulness)
	}
	if faithfulness.MeanScore != 0.75 || faithfulness.MeanTokens != 20 || faithfulness.MeanLatency != 200 ||
		faithfulness.P95Tokens != 30 || faithfulness.P95Latency != 300 {
		t.Fatalf("unexpected metric means: %+v", faithfulness)
	}
	if faithfulness.MinScore != 0.5 || faithfulness.MaxScore != 1.0 {
		t.Fatalf("unexpected min/max: %+v", faithfulness)
	}
	if summary.ByTier["critical"].Count != 2 ||
		summary.ByFlow["rag.answer"].Count != 2 ||
		summary.ByDataset["smoke/v1"].Count != 2 ||
		len(summary.ByCase) != 3 {
		t.Fatalf("unexpected grouped summaries: tiers=%+v flows=%+v datasets=%+v cases=%+v", summary.ByTier, summary.ByFlow, summary.ByDataset, summary.ByCase)
	}
}

func TestSummarizeSkipsScenarioSummaryRows(t *testing.T) {
	summary := Summarize([]eval.RunResult{
		{Metric: "Faithfulness", Score: 1.0, Passed: true, Tokens: 10},
		{
			Kind:   "scenario_summary",
			Metric: "_scenario_summary",
			Score:  1.0,
			Passed: true,
			ScenarioSummary: &eval.ScenarioSummary{
				Passed:   true,
				RunCount: 3,
				PassRuns: 2,
			},
		},
	})

	if summary.Total != 1 || summary.Passed != 1 {
		t.Fatalf("scenario summary rows should not count as metric rows: %+v", summary)
	}
	if summary.ScenarioTotal != 1 || summary.ScenarioPassed != 1 ||
		summary.ScenarioRuns != 3 || summary.ScenarioPassRuns != 2 {
		t.Fatalf("scenario summary should be tracked separately: %+v", summary)
	}
	if _, ok := summary.ByMetric["_scenario_summary"]; ok {
		t.Fatalf("scenario summary metric should be skipped: %+v", summary.ByMetric)
	}
}

func TestSummarizeWithOptionsReportsFlakyIdentities(t *testing.T) {
	summary := SummarizeWithOptions([]eval.RunResult{
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.9, Passed: true, Metadata: map[string]any{"case_id": "a"}},
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.4, Passed: false, Metadata: map[string]any{"case_id": "a"}},
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.91, Passed: true, Metadata: map[string]any{"case_id": "b"}},
		{TestName: "TestEval", Metric: "Faithfulness", Score: 0.9, Passed: true, Metadata: map[string]any{"case_id": "b"}},
	}, SummaryOptions{FlakyScoreStdDev: 0.2})

	if len(summary.Flaky) != 1 {
		t.Fatalf("expected one flaky identity, got %+v", summary.Flaky)
	}
	flaky := summary.Flaky[0]
	if flaky.Count != 2 || flaky.Passed != 1 || flaky.Failed != 1 || !flaky.MixedPass {
		t.Fatalf("unexpected flaky summary: %+v", flaky)
	}
	if flaky.Identity.TestName != "" || flaky.Identity.CaseName != "a" || flaky.Identity.Metric != "Faithfulness" {
		t.Fatalf("unexpected flaky identity: %+v", flaky.Identity)
	}
}

func TestSummarizeWithPolicyUsesPolicyFlakyThreshold(t *testing.T) {
	results := []eval.RunResult{
		{TestName: "TestEval/old", Metric: "Faithfulness", Score: 0.9, Passed: true, Metadata: map[string]any{"case_id": "same"}},
		{TestName: "TestEval/new", Metric: "Faithfulness", Score: 0.7, Passed: true, Metadata: map[string]any{"case_id": "same"}},
	}

	relaxed := SummarizeWithPolicy(results, Policy{
		CaseIDKey: "case_id",
		Default: MetricPolicy{
			FlakyScoreStdDev: floatPtr(0.2),
		},
	})
	if len(relaxed.Flaky) != 0 {
		t.Fatalf("default policy threshold should suppress score-only flake: %+v", relaxed.Flaky)
	}
	if relaxed.ByCase["same/Faithfulness"].Count != 2 {
		t.Fatalf("case-id policy should group renamed tests by stable case ID: %+v", relaxed.ByCase)
	}

	sensitive := SummarizeWithPolicy(results, Policy{
		CaseIDKey: "case_id",
		Default: MetricPolicy{
			FlakyScoreStdDev: floatPtr(0.2),
		},
		Metrics: map[string]MetricPolicy{
			"Faithfulness": {FlakyScoreStdDev: floatPtr(0.05)},
		},
	})
	if len(sensitive.Flaky) != 1 || !sensitive.Flaky[0].ScoreFlaky {
		t.Fatalf("metric policy threshold should mark score flake: %+v", sensitive.Flaky)
	}
}

func TestSummarizeFileReadsJSONL(t *testing.T) {
	path := writeJSONL(t,
		`{"test_name":"TestA","metric":"Faithfulness","score":1,"passed":true}`+"\n"+
			`{"test_name":"TestB","metric":"Faithfulness","score":0,"passed":false}`+"\n",
	)

	summary, err := SummarizeFile(path)
	if err != nil {
		t.Fatalf("SummarizeFile: %v", err)
	}
	if summary.Total != 2 || summary.ByMetric["Faithfulness"].Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestSummarizeFileMalformedJSONL(t *testing.T) {
	path := writeJSONL(t, `{"metric":`)
	_, err := SummarizeFile(path)
	if err == nil || !strings.Contains(err.Error(), "jsonl line 1") {
		t.Fatalf("expected malformed JSONL error, got %v", err)
	}
}

func writeJSONL(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := dir + "/results.jsonl"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
