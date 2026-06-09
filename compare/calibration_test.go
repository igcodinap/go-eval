package compare

import (
	"math"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestCalibrateReportsJudgeDisagreement(t *testing.T) {
	report := Calibrate([]eval.RunResult{
		{
			TestName: "TestEval/old",
			Metric:   "Faithfulness",
			Score:    0.9,
			Passed:   true,
			Reason:   "good",
			Metadata: map[string]any{"case_id": "same", "judge": "judge-a", "tier": "critical", "flow": "rag", "dataset": "smoke/v1"},
		},
		{
			TestName: "TestEval/new",
			Metric:   "Faithfulness",
			Score:    0.4,
			Passed:   false,
			Reason:   "bad",
			Metadata: map[string]any{"case_id": "same", "judge": "judge-b", "tier": "critical", "flow": "rag", "dataset": "smoke/v1"},
		},
	}, CalibrationOptions{CaseIDKey: "case_id", JudgeKey: "judge", ScoreTolerance: 0.1})

	if report.Summary.TotalGroups != 1 || report.Summary.DisagreementGroups != 1 || report.Summary.JudgeCount != 2 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	got := report.Disagreements[0]
	if got.Identity.CaseName != "same" || !got.PassDisagreement || !got.ScoreDisagreement || got.ScoreRange != 0.5 {
		t.Fatalf("unexpected disagreement: %+v", got)
	}
	if got.Tier != "critical" || got.Flow != "rag" || got.Dataset != "smoke/v1" {
		t.Fatalf("metadata not preserved: %+v", got)
	}
}

func TestCalibrateAggregatesDuplicateJudgeRows(t *testing.T) {
	report := Calibrate([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.8, Passed: true, Reason: "a1", Metadata: map[string]any{"case_id": "same", "judge": "judge-a"}},
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.6, Passed: true, Reason: "a2", Metadata: map[string]any{"case_id": "same", "judge": "judge-a"}},
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.4, Passed: false, Reason: "b1", Metadata: map[string]any{"case_id": "same", "judge": "judge-b"}},
	}, CalibrationOptions{CaseIDKey: "case_id", JudgeKey: "judge", ScoreTolerance: 0.1})

	if report.Summary.DisagreementGroups != 1 {
		t.Fatalf("expected one disagreement, got %+v", report)
	}
	got := report.Disagreements[0]
	if len(got.Judges) != 2 {
		t.Fatalf("expected two judge aggregates, got %+v", got.Judges)
	}
	if got.Judges[0].Judge != "judge-a" || got.Judges[0].Count != 2 || got.Judges[0].Passes != 2 || got.Judges[0].Score != 0.7 {
		t.Fatalf("duplicate judge rows were not averaged: %+v", got.Judges[0])
	}
}

func TestCalibrateSuppressesWithinToleranceScoreOnlyDisagreement(t *testing.T) {
	report := Calibrate([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.90, Passed: true, Metadata: map[string]any{"judge": "a"}},
		{TestName: "TestEval/a", Metric: "Faithfulness", Score: 0.88, Passed: true, Metadata: map[string]any{"judge": "b"}},
	}, CalibrationOptions{ScoreTolerance: 0.05})

	if len(report.Disagreements) != 0 {
		t.Fatalf("expected no disagreement, got %+v", report.Disagreements)
	}
}

func TestCalibratePairwiseVariants(t *testing.T) {
	report := Calibrate([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.9, Passed: true, Metadata: map[string]any{"variant": "A"}},
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.7, Passed: true, Metadata: map[string]any{"variant": "B"}},
		{TestName: "TestEval/b", Metric: "AnswerCorrectness", Score: 0.4, Passed: false, Metadata: map[string]any{"variant": "A"}},
		{TestName: "TestEval/b", Metric: "AnswerCorrectness", Score: 0.8, Passed: true, Metadata: map[string]any{"variant": "B"}},
	}, CalibrationOptions{VariantKey: "variant", ScoreTolerance: 0.01})

	if len(report.Pairwise) != 1 {
		t.Fatalf("expected one pairwise report, got %+v", report.Pairwise)
	}
	got := report.Pairwise[0]
	if got.Left != "A" || got.Right != "B" || got.Count != 2 || got.LeftWins != 1 || got.RightWins != 1 {
		t.Fatalf("unexpected pairwise report: %+v", got)
	}
}

func TestCalibratePairwiseAggregatesDuplicateVariants(t *testing.T) {
	report := Calibrate([]eval.RunResult{
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.8, Passed: true, Metadata: map[string]any{"variant": "A"}},
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.2, Passed: false, Metadata: map[string]any{"variant": "A"}},
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.4, Passed: true, Metadata: map[string]any{"variant": "B"}},
		{TestName: "TestEval/a", Metric: "AnswerCorrectness", Score: 0.4, Passed: true, Metadata: map[string]any{"variant": "B"}},
	}, CalibrationOptions{VariantKey: "variant", ScoreTolerance: 0.01})

	if len(report.Pairwise) != 1 {
		t.Fatalf("expected one pairwise report, got %+v", report.Pairwise)
	}
	got := report.Pairwise[0]
	if got.Left != "A" || got.Right != "B" || got.LeftWins != 1 || got.RightWins != 0 || math.Abs(got.MeanScoreDelta-0.1) > 1e-9 {
		t.Fatalf("duplicate variants were not averaged: %+v", got)
	}
}
