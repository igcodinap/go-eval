package compare

import (
	"os"
	"strings"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestSummarizeAggregatesByMetric(t *testing.T) {
	summary := Summarize([]eval.RunResult{
		{Metric: "Faithfulness", Score: 1.0, Passed: true, Tokens: 10, LatencyNS: 100},
		{Metric: "Faithfulness", Score: 0.5, Passed: false, Tokens: 30, LatencyNS: 300},
		{Metric: "ToolCallF1", Score: 0.75, Passed: true, Tokens: 0, LatencyNS: 0},
	})

	if summary.Total != 3 || summary.Passed != 2 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	faithfulness := summary.ByMetric["Faithfulness"]
	if faithfulness.Count != 2 || faithfulness.Passed != 1 || faithfulness.Failed != 1 {
		t.Fatalf("unexpected metric summary: %+v", faithfulness)
	}
	if faithfulness.MeanScore != 0.75 || faithfulness.MeanTokens != 20 || faithfulness.MeanLatency != 200 {
		t.Fatalf("unexpected metric means: %+v", faithfulness)
	}
	if faithfulness.MinScore != 0.5 || faithfulness.MaxScore != 1.0 {
		t.Fatalf("unexpected min/max: %+v", faithfulness)
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
