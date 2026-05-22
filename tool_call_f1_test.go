package eval

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolCallF1ReportsPrecisionRecallAndF1(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{{Name: "search"}, {Name: "lookup"}, {Name: "extra"}},
		[]ToolCall{{Name: "search"}, {Name: "lookup"}, {Name: "missing"}},
	)

	result, err := (ToolCallF1{Threshold: 0.5}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected F1 to pass threshold, got %+v", result)
	}
	if result.Score != 2.0/3.0 {
		t.Fatalf("score = %v, want 2/3", result.Score)
	}
	if len(result.Dimensions) != 3 {
		t.Fatalf("expected three dimensions, got %+v", result.Dimensions)
	}
	if result.Dimensions[0].Name != "precision" || result.Dimensions[0].Score != 2.0/3.0 {
		t.Fatalf("unexpected precision dimension: %+v", result.Dimensions[0])
	}
	if result.Dimensions[1].Name != "recall" || result.Dimensions[1].Score != 2.0/3.0 {
		t.Fatalf("unexpected recall dimension: %+v", result.Dimensions[1])
	}
	if result.Dimensions[2].Name != "f1" || result.Dimensions[2].Score != 2.0/3.0 {
		t.Fatalf("unexpected f1 dimension: %+v", result.Dimensions[2])
	}
}

func TestToolCallF1MatchesArguments(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{
			{Name: "search", Arguments: json.RawMessage(`{"query":"france"}`)},
			{Name: "search", Arguments: json.RawMessage(`{"query":"italy"}`)},
		},
		[]ToolCall{
			{Name: "search", Arguments: json.RawMessage(`{"query":"france"}`)},
			{Name: "search", Arguments: json.RawMessage(`{"query":"spain"}`)},
		},
	)

	result, err := (ToolCallF1{MatchArgs: true, Threshold: 0.5}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 0.5 {
		t.Fatalf("result = %+v, want score 0.5 pass", result)
	}
}

func TestToolCallF1EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		actual   []ToolCall
		expected []ToolCall
		want     float64
		passed   bool
	}{
		{name: "both empty", want: 1, passed: true},
		{name: "actual empty", expected: []ToolCall{{Name: "search"}}, want: 0, passed: false},
		{name: "expected empty", actual: []ToolCall{{Name: "search"}}, want: 0, passed: false},
		{name: "duplicate missing", actual: []ToolCall{{Name: "search"}}, expected: []ToolCall{{Name: "search"}, {Name: "search"}}, want: 2.0 / 3.0, passed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (ToolCallF1{}).Score(context.Background(), nil, caseWithCalls(tt.actual, tt.expected))
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if result.Score != tt.want || result.Passed != tt.passed {
				t.Fatalf("result = %+v, want score %v passed %v", result, tt.want, tt.passed)
			}
		})
	}
}
