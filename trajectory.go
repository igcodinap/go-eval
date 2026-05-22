package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MatchMode controls how expected tool calls are matched against actual calls.
type MatchMode string

const (
	// MatchStrict requires the same calls in the same order.
	MatchStrict MatchMode = "strict"
	// MatchUnordered requires the same multiset of calls, ignoring order.
	MatchUnordered MatchMode = "unordered"
	// MatchSubset requires expected calls to appear in actual calls; extras are allowed.
	MatchSubset MatchMode = "subset"
	// MatchSuperset requires actual calls to be allowed by expected calls; omissions are allowed.
	MatchSuperset MatchMode = "superset"
)

type toolCallMatchOptions struct {
	matchArgs   bool
	matchResult bool
}

func flattenToolCalls(turns []Turn) []ToolCall {
	var calls []ToolCall
	for _, turn := range turns {
		calls = append(calls, turn.ToolCalls...)
	}
	return calls
}

func normalizeMatchMode(mode MatchMode) (MatchMode, error) {
	if mode == "" {
		return MatchStrict, nil
	}
	switch mode {
	case MatchStrict, MatchUnordered, MatchSubset, MatchSuperset:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported match mode %q", mode)
	}
}

func countToolCallMatches(actual []ToolCall, expected []ToolCall, opts toolCallMatchOptions) (int, error) {
	matchedActual := make([]bool, len(actual))
	count := 0

	for _, expectedIndex := range orderedExpectedIndices(expected, opts) {
		for actualIndex := range actual {
			if matchedActual[actualIndex] {
				continue
			}
			matched, err := toolCallsMatch(actual[actualIndex], expected[expectedIndex], opts)
			if err != nil {
				return 0, err
			}
			if matched {
				matchedActual[actualIndex] = true
				count++
				break
			}
		}
	}

	return count, nil
}

func orderedExpectedIndices(expected []ToolCall, opts toolCallMatchOptions) []int {
	indices := make([]int, len(expected))
	for i := range expected {
		indices[i] = i
	}
	if !opts.matchArgs && !opts.matchResult {
		return indices
	}

	// Match more specific expected calls first so wildcard calls do not consume
	// actual calls that should satisfy exact expectations.
	for i := 0; i < len(indices)-1; i++ {
		for j := i + 1; j < len(indices); j++ {
			if expectedSpecificity(expected[indices[j]], opts) > expectedSpecificity(expected[indices[i]], opts) {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}
	return indices
}

func expectedSpecificity(call ToolCall, opts toolCallMatchOptions) int {
	score := 0
	if opts.matchArgs && len(call.Arguments) > 0 {
		score++
	}
	if opts.matchResult && call.Result != "" {
		score++
	}
	return score
}

func toolCallsMatch(actual ToolCall, expected ToolCall, opts toolCallMatchOptions) (bool, error) {
	if actual.Name != expected.Name {
		return false, nil
	}
	if opts.matchArgs && len(expected.Arguments) > 0 {
		matched, err := rawJSONEqual(actual.Arguments, expected.Arguments)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	if opts.matchResult && expected.Result != "" && actual.Result != expected.Result {
		return false, nil
	}
	return true, nil
}

func rawJSONEqual(actual json.RawMessage, expected json.RawMessage) (bool, error) {
	if len(actual) == 0 {
		return false, nil
	}

	actualNormalized, err := normalizeJSON(actual)
	if err != nil {
		return false, fmt.Errorf("actual arguments are not valid JSON: %w", err)
	}
	expectedNormalized, err := normalizeJSON(expected)
	if err != nil {
		return false, fmt.Errorf("expected arguments are not valid JSON: %w", err)
	}
	return bytes.Equal(actualNormalized, expectedNormalized), nil
}

func normalizeJSON(raw json.RawMessage) ([]byte, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureSingleJSONValue(dec); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func safeRatio(numerator int, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}
