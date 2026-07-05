package eval

import (
	"context"
	"testing"
)

func TestRubricScoresAndAddsMetadata(t *testing.T) {
	judge := &MockJudge{Response: JudgeResponse{Score: 0.9, Reason: "meets rubric"}}
	metric := Rubric{
		ID:        "answer-quality",
		Version:   "v1",
		Criteria:  "Answer directly and accurately.",
		Threshold: 0.8,
	}

	result, err := metric.Score(context.Background(), judge, Case{
		Input:    "Question",
		Output:   "Answer",
		Metadata: map[string]any{"case_id": "case-1"},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Metric != "Rubric(answer-quality)" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Metadata["rubric_id"] != "answer-quality" || result.Metadata["rubric_version"] != "v1" || result.Metadata["case_id"] != "case-1" {
		t.Fatalf("unexpected metadata: %+v", result.Metadata)
	}
}

func TestRubricRequiresIDAndCriteria(t *testing.T) {
	if _, err := (Rubric{Criteria: "x"}).Score(context.Background(), &MockJudge{}, Case{}); err == nil {
		t.Fatalf("expected missing ID error")
	}
	if _, err := (Rubric{ID: "x"}).Score(context.Background(), &MockJudge{}, Case{}); err == nil {
		t.Fatalf("expected missing Criteria error")
	}
}
