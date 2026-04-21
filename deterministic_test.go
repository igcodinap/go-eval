package eval

import (
	"context"
	"strings"
	"testing"
)

func TestContains_Found(t *testing.T) {
	r, err := Contains{}.Score(context.Background(), nil, Case{
		Output:   "Paris is the capital of France.",
		Expected: "capital of France",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestContains_NotFound(t *testing.T) {
	r, err := Contains{}.Score(context.Background(), nil, Case{
		Output:   "Paris is the capital of France.",
		Expected: "capital of Spain",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRegex_Match(t *testing.T) {
	r, err := Regex{Pattern: `capital of \w+`}.Score(context.Background(), nil, Case{
		Output: "Paris is the capital of France.",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRegex_NoMatch(t *testing.T) {
	r, err := Regex{Pattern: `capital of Spain`}.Score(context.Background(), nil, Case{
		Output: "Paris is the capital of France.",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestRegex_InvalidPattern(t *testing.T) {
	r, err := Regex{Pattern: `[`}.Score(context.Background(), nil, Case{
		Output: "x",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_TopLevelKey(t *testing.T) {
	m, err := NewJSONPath("pickup_city")
	if err != nil {
		t.Fatalf("NewJSONPath: %v", err)
	}

	r, err := m.Score(context.Background(), nil, Case{
		Output:   `{"pickup_city":"Santiago"}`,
		Expected: "Santiago",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_NestedKey(t *testing.T) {
	m, err := NewJSONPath("fields.adults")
	if err != nil {
		t.Fatalf("NewJSONPath: %v", err)
	}

	r, err := m.Score(context.Background(), nil, Case{
		Output:   `{"fields":{"adults":2}}`,
		Expected: "2",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_ArrayIndex(t *testing.T) {
	m, err := NewJSONPath("items[0].name")
	if err != nil {
		t.Fatalf("NewJSONPath: %v", err)
	}

	r, err := m.Score(context.Background(), nil, Case{
		Output:   `{"items":[{"name":"A"}]}`,
		Expected: "A",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_KeyNotFound(t *testing.T) {
	m, err := NewJSONPath("pickup_city")
	if err != nil {
		t.Fatalf("NewJSONPath: %v", err)
	}

	r, err := m.Score(context.Background(), nil, Case{
		Output:   `{"dropoff_city":"Santiago"}`,
		Expected: "Santiago",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_InvalidJSON(t *testing.T) {
	m, err := NewJSONPath("pickup_city")
	if err != nil {
		t.Fatalf("NewJSONPath: %v", err)
	}

	r, err := m.Score(context.Background(), nil, Case{
		Output:   `{"pickup_city":`,
		Expected: "Santiago",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestJSONPath_InvalidPath(t *testing.T) {
	if _, err := NewJSONPath(`items[*].name`); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestJSONPath_EmptyPathScoreFails(t *testing.T) {
	r, err := (JSONPath{}).Score(context.Background(), nil, Case{
		Output:   `{"a":1}`,
		Expected: "1",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestFieldCount_Enough(t *testing.T) {
	r, err := FieldCount{MinFields: 3}.Score(context.Background(), nil, Case{
		Output: `{"a":1,"b":2,"c":3}`,
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestFieldCount_TooFew(t *testing.T) {
	r, err := FieldCount{MinFields: 3}.Score(context.Background(), nil, Case{
		Output: `{"a":1}`,
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Score != 1.0/3.0 {
		t.Fatalf("score mismatch: got %.6f", r.Score)
	}
}

func TestFieldCount_NullValuesSkipped(t *testing.T) {
	r, err := FieldCount{MinFields: 2}.Score(context.Background(), nil, Case{
		Output: `{"a":1,"b":null}`,
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Score != 0.5 {
		t.Fatalf("score mismatch: got %.6f", r.Score)
	}
}

func TestFieldCount_TrailingJSON(t *testing.T) {
	r, err := FieldCount{MinFields: 1}.Score(context.Background(), nil, Case{
		Output: `{"a":1} {"b":2}`,
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("expected failure for trailing JSON, got %+v", r)
	}
	if !strings.Contains(r.Reason, "trailing") && !strings.Contains(r.Reason, "not a valid JSON object") {
		t.Fatalf("reason missing trailing-data context: %q", r.Reason)
	}
}
