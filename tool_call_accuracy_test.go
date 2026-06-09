package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolCallAccuracyModes(t *testing.T) {
	actual := []ToolCall{{Name: "search"}, {Name: "lookup"}}
	expected := []ToolCall{{Name: "search"}, {Name: "lookup"}}

	tests := []struct {
		name     string
		mode     MatchMode
		actual   []ToolCall
		expected []ToolCall
		want     float64
		passed   bool
	}{
		{name: "strict exact", mode: MatchStrict, actual: actual, expected: expected, want: 1, passed: true},
		{name: "strict order mismatch", mode: MatchStrict, actual: reverseToolCalls(actual), expected: expected, want: 0, passed: false},
		{name: "unordered order ignored", mode: MatchUnordered, actual: reverseToolCalls(actual), expected: expected, want: 1, passed: true},
		{name: "unordered extra actual", mode: MatchUnordered, actual: append(actual, ToolCall{Name: "extra"}), expected: expected, want: 2.0 / 3.0, passed: false},
		{name: "subset allows extra actual", mode: MatchSubset, actual: append(actual, ToolCall{Name: "extra"}), expected: expected, want: 1, passed: true},
		{name: "subset missing expected", mode: MatchSubset, actual: []ToolCall{{Name: "search"}}, expected: expected, want: 0.5, passed: false},
		{name: "superset allows omitted expected", mode: MatchSuperset, actual: []ToolCall{{Name: "search"}}, expected: expected, want: 1, passed: true},
		{name: "superset rejects extra actual", mode: MatchSuperset, actual: append(actual, ToolCall{Name: "extra"}), expected: expected, want: 2.0 / 3.0, passed: false},
		{name: "both empty", mode: MatchStrict, actual: nil, expected: nil, want: 1, passed: true},
		{name: "expected empty subset", mode: MatchSubset, actual: []ToolCall{{Name: "search"}}, expected: nil, want: 1, passed: true},
		{name: "actual empty superset", mode: MatchSuperset, actual: nil, expected: expected, want: 1, passed: true},
		{name: "duplicates counted", mode: MatchUnordered, actual: []ToolCall{{Name: "search"}}, expected: []ToolCall{{Name: "search"}, {Name: "search"}}, want: 0.5, passed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (ToolCallAccuracy{Mode: tt.mode}).Score(context.Background(), nil, caseWithCalls(tt.actual, tt.expected))
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if result.Score != tt.want || result.Passed != tt.passed {
				t.Fatalf("result = %+v, want score %v passed %v", result, tt.want, tt.passed)
			}
		})
	}
}

func TestToolCallAccuracyMatchesArgumentsAndResults(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{{
			Name:      "search",
			Arguments: json.RawMessage(`{"query":"capital of France","limit":1}`),
			Result:    "Paris",
		}},
		[]ToolCall{{
			Name:      "search",
			Arguments: json.RawMessage(`{"limit":1,"query":"capital of France"}`),
			Result:    "Paris",
		}},
	)

	result, err := (ToolCallAccuracy{MatchArgs: true, MatchResult: true}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected argument/result match, got %+v", result)
	}
}

func TestToolCallAccuracyMatchesResultOnly(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{{Name: "search", Result: "Paris"}},
		[]ToolCall{{Name: "search", Result: "Lyon"}},
	)

	result, err := (ToolCallAccuracy{MatchResult: true}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0 {
		t.Fatalf("expected result mismatch to fail, got %+v", result)
	}
}

func TestToolCallAccuracyExpectedEmptyArgumentsAreWildcard(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{{Name: "search", Arguments: json.RawMessage(`{"query":"anything"}`)}},
		[]ToolCall{{Name: "search"}},
	)

	result, err := (ToolCallAccuracy{MatchArgs: true}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected wildcard arguments to pass, got %+v", result)
	}
}

func TestToolCallAccuracyInvalidArgumentsFailMetric(t *testing.T) {
	c := caseWithCalls(
		[]ToolCall{{Name: "search", Arguments: json.RawMessage(`{`)}},
		[]ToolCall{{Name: "search", Arguments: json.RawMessage(`{"query":"x"}`)}},
	)

	result, err := (ToolCallAccuracy{MatchArgs: true}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "actual arguments are not valid JSON") {
		t.Fatalf("expected invalid JSON failure, got %+v", result)
	}
}

func TestToolCallAccuracyInvalidModeFailsMetric(t *testing.T) {
	result, err := (ToolCallAccuracy{Mode: "near-enough"}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, `unsupported match mode "near-enough"`) {
		t.Fatalf("expected invalid mode failure, got %+v", result)
	}
}

func TestToolCallAccuracyUsesTraceToolCallsWhenPresent(t *testing.T) {
	c := caseWithTraceCalls(
		[]ToolCall{{Name: "trace_search"}},
		[]ToolCall{{Name: "trace_search"}},
	)
	c.Turns = []Turn{{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "turn_search"}}}}

	result, err := (ToolCallAccuracy{}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected trace call to satisfy accuracy, got %+v", result)
	}
}

func caseWithCalls(actual []ToolCall, expected []ToolCall) Case {
	return Case{
		Turns: []Turn{
			{Role: RoleAssistant, ToolCalls: actual},
		},
		ExpectedToolCalls: expected,
	}
}

func caseWithTraceCalls(actual []ToolCall, expected []ToolCall) Case {
	spans := make([]Span, len(actual))
	for i := range actual {
		call := actual[i]
		spans[i] = Span{
			Kind:     "tool_call",
			ToolCall: &call,
		}
	}
	return Case{
		Trace:             &Trace{ID: "trace-1", Spans: spans},
		ExpectedToolCalls: expected,
	}
}

func reverseToolCalls(calls []ToolCall) []ToolCall {
	out := make([]ToolCall, len(calls))
	for i := range calls {
		out[i] = calls[len(calls)-1-i]
	}
	return out
}
