package eval

import (
	"context"
	"math"
	"testing"
)

type rawSequenceJudge struct {
	responses []string
	calls     int
	prompts   []string
}

func (j *rawSequenceJudge) Evaluate(ctx context.Context, prompt string) (JudgeResponse, error) {
	_ = ctx
	_ = prompt
	return JudgeResponse{}, nil
}

func (j *rawSequenceJudge) EvaluateRaw(ctx context.Context, prompt string) (RawJudgeResponse, error) {
	_ = ctx
	j.prompts = append(j.prompts, prompt)
	idx := j.calls
	if idx >= len(j.responses) {
		idx = len(j.responses) - 1
	}
	j.calls++
	return RawJudgeResponse{Content: j.responses[idx], Tokens: 10}, nil
}

func TestCompound_AllPass(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "Is the answer in Spanish?", Threshold: 0.7},
			{Name: "helpfulness", Rubric: "Is the answer useful?", Threshold: 0.6},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":{"score":0.8,"reason":"ok"},"helpfulness":{"score":0.9,"reason":"great"}}`,
		},
	}

	r, err := m.Score(context.Background(), j, Case{Input: "x", Output: "y"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected Passed=true, got %+v", r)
	}
	if math.Abs(r.Score-0.85) > 1e-9 {
		t.Fatalf("unexpected Score: %.4f", r.Score)
	}
	if len(r.Dimensions) != 2 {
		t.Fatalf("expected 2 dimensions, got %+v", r.Dimensions)
	}
}

func TestCompound_OneFails(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
			{Name: "helpfulness", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":{"score":0.8,"reason":"ok"},"helpfulness":{"score":0.6,"reason":"weak"}}`,
		},
	}

	r, err := m.Score(context.Background(), j, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("expected Passed=false, got %+v", r)
	}
	if r.Reason == "" || r.Reason == "language_match=0.80 helpfulness=0.60" {
		t.Fatalf("expected failing dimension to be called out, got %q", r.Reason)
	}
}

func TestCompound_SkippedDimension(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
			{Name: "style", Rubric: "x", Threshold: 0},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":{"score":0.8,"reason":"ok"},"style":{"score":0.1,"reason":"poor"}}`,
		},
	}

	r, err := m.Score(context.Background(), j, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected skip threshold to not fail run, got %+v", r)
	}
}

func TestCompound_FlatFormatRejected(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":0.8}`,
		},
	}

	if _, err := m.Score(context.Background(), j, Case{}); err == nil {
		t.Fatalf("expected flat response parsing error")
	}
}

func TestCompound_MarkdownFenceStripping(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			"```json\n{\"language_match\":{\"score\":0.8,\"reason\":\"ok\"}}\n```",
		},
	}

	r, err := m.Score(context.Background(), j, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected Passed=true, got %+v", r)
	}
}

func TestCompound_OutOfRangeRejected(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":{"score":1.3,"reason":"bad"}}`,
		},
	}

	if _, err := m.Score(context.Background(), j, Case{}); err == nil {
		t.Fatalf("expected out-of-range score error")
	}
}

func TestCompound_MissingDimensionRejected(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
			{Name: "helpfulness", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			`{"language_match":{"score":0.9,"reason":"ok"}}`,
		},
	}

	if _, err := m.Score(context.Background(), j, Case{}); err == nil {
		t.Fatalf("expected missing dimension error")
	}
}

func TestCompound_RetryOnceOnParseFailure(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
		},
	}
	j := &rawSequenceJudge{
		responses: []string{
			"not-json",
			`{"language_match":{"score":0.8,"reason":"ok"}}`,
		},
	}

	r, err := m.Score(context.Background(), j, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed {
		t.Fatalf("expected Passed=true, got %+v", r)
	}
	if j.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", j.calls)
	}
	if len(j.prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(j.prompts))
	}
}

func TestCompound_RequiresRawJudge(t *testing.T) {
	m := Compound{
		Dimensions: []Dimension{
			{Name: "language_match", Rubric: "x", Threshold: 0.7},
		},
	}

	if _, err := m.Score(context.Background(), &MockJudge{}, Case{}); err == nil {
		t.Fatalf("expected RawJudge requirement error")
	}
}
