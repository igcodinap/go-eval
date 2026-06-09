package compare

import (
	"strings"
	"testing"
	"time"

	eval "github.com/igcodinap/go-eval"
)

func TestReportMarkdownIncludesSummaryAndComparison(t *testing.T) {
	summary := Summarize([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 1, Passed: true, Tokens: 10, LatencyNS: int64(time.Millisecond)},
	})
	comparison := Compare(
		[]eval.RunResult{{TestName: "TestEval/a", Metric: "Faithfulness", Score: 1, Passed: true}},
		[]eval.RunResult{{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0, Passed: false}},
	)
	rendered, err := ReportMarkdown(NewComparisonReport("old.jsonl", "new.jsonl", summary, comparison))
	if err != nil {
		t.Fatalf("ReportMarkdown: %v", err)
	}
	out := string(rendered)
	for _, want := range []string{"# go-eval report", "## Metrics", "Faithfulness", "## Comparison", "regressed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown missing %q:\n%s", want, out)
		}
	}
}

func TestReportHTMLIncludesEscapedMetricRows(t *testing.T) {
	summary := Summarize([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "Faithfulness<script>", Score: 1, Passed: true},
	})
	rendered, err := ReportHTML(NewResultsReport("results.jsonl", summary))
	if err != nil {
		t.Fatalf("ReportHTML: %v", err)
	}
	out := string(rendered)
	if !strings.Contains(out, "go-eval report") || !strings.Contains(out, "Faithfulness&lt;script&gt;") {
		t.Fatalf("html missing escaped content:\n%s", out)
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	rendered, err := ReportJSON(NewResultsReport("results.jsonl", ResultsSummary{Total: 1}))
	if err != nil {
		t.Fatalf("ReportJSON: %v", err)
	}
	if !strings.Contains(string(rendered), `"ResultsPath": "results.jsonl"`) {
		t.Fatalf("json missing results path:\n%s", rendered)
	}
}
