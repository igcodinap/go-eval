package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Contains checks whether Case.Output contains Case.Expected.
type Contains struct{}

// Name implements Metric.
func (m Contains) Name() string { return "Contains" }

// Score implements Metric.
func (m Contains) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	passed := strings.Contains(c.Output, c.Expected)
	if passed {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: "output contains expected substring",
		}, nil
	}

	return Result{
		Score:  0.0,
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("output does not contain expected substring %q", c.Expected),
	}, nil
}

// Regex checks whether Case.Output matches Pattern.
type Regex struct {
	Pattern string
}

// Name implements Metric.
func (m Regex) Name() string { return "Regex" }

// Score implements Metric.
func (m Regex) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	re, err := regexp.Compile(m.Pattern)
	if err != nil {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: fmt.Sprintf("invalid regex pattern %q: %v", m.Pattern, err),
		}, nil
	}

	passed := re.MatchString(c.Output)
	if passed {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: "output matches regex pattern",
		}, nil
	}

	return Result{
		Score:  0.0,
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("output does not match regex pattern %q", m.Pattern),
	}, nil
}

var (
	jsonPathRegex     = regexp.MustCompile(`^[A-Za-z_]\w*(\[\d+\])*(\.[A-Za-z_]\w*(\[\d+\])*)*$`)
	jsonPathSegmentRE = regexp.MustCompile(`^([A-Za-z_]\w*)((?:\[\d+\])*)$`)
	jsonPathIndexRE   = regexp.MustCompile(`\[(\d+)\]`)
)

type jsonPathStep struct {
	key     string
	indices []int
}

// JSONPath extracts a value from Case.Output and compares it to Case.Expected.
//
// Use NewJSONPath or MustJSONPath to construct it.
type JSONPath struct {
	path string
}

// NewJSONPath validates and returns a JSONPath metric.
func NewJSONPath(path string) (JSONPath, error) {
	if !jsonPathRegex.MatchString(path) {
		return JSONPath{}, fmt.Errorf("unsupported JSONPath %q: supported syntax is dot-separated keys with optional [index], e.g. fields.adults or items[0].name", path)
	}
	return JSONPath{path: path}, nil
}

// MustJSONPath returns a validated JSONPath or panics.
func MustJSONPath(path string) JSONPath {
	out, err := NewJSONPath(path)
	if err != nil {
		panic(err)
	}
	return out
}

// Path returns the configured JSON path.
func (m JSONPath) Path() string { return m.path }

// Name implements Metric.
func (m JSONPath) Name() string { return "JSONPath" }

// Score implements Metric.
func (m JSONPath) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if m.path == "" {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: "path is empty; construct JSONPath with NewJSONPath or MustJSONPath",
		}, nil
	}

	steps, err := parseJSONPathSteps(m.path)
	if err != nil {
		//nolint:nilerr // Invalid path is represented as a failed metric result, not an execution error.
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: err.Error(),
		}, nil
	}

	payload, err := decodeJSONAny(c.Output)
	if err != nil {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: fmt.Sprintf("output is not valid JSON: %v", err),
		}, nil
	}

	value, ok, reason := extractJSONPathValue(payload, steps)
	if !ok {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: reason,
		}, nil
	}

	actual := stringifyJSONValue(value)
	passed := actual == c.Expected
	if passed {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("path %q matched expected value", m.path),
		}, nil
	}

	return Result{
		Score:  0.0,
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("path %q mismatch: got %q, expected %q", m.path, actual, c.Expected),
	}, nil
}

// FieldCount checks minimum non-null top-level JSON fields.
type FieldCount struct {
	MinFields int
}

// Name implements Metric.
func (m FieldCount) Name() string { return "FieldCount" }

// Score implements Metric.
func (m FieldCount) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if m.MinFields <= 0 {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: fmt.Sprintf("MinFields must be >= 1, got %d", m.MinFields),
		}, nil
	}

	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(c.Output))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: fmt.Sprintf("output is not valid JSON object: %v", err),
		}, nil
	}
	if payload == nil {
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: "output is not a valid JSON object: null top-level value",
		}, nil
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		reason := "output is not a valid JSON object: trailing data after top-level value"
		if err != nil && err != io.EOF {
			reason = fmt.Sprintf("output is not a valid JSON object: %v", err)
		}
		return Result{
			Score:  0.0,
			Passed: false,
			Metric: m.Name(),
			Reason: reason,
		}, nil
	}

	count := 0
	for _, v := range payload {
		if v != nil {
			count++
		}
	}

	score := float64(count) / float64(m.MinFields)
	if score > 1.0 {
		score = 1.0
	}

	passed := count >= m.MinFields
	if passed {
		return Result{
			Score:  score,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("non-null top-level field count %d meets minimum %d", count, m.MinFields),
		}, nil
	}

	return Result{
		Score:  score,
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("non-null top-level field count %d below minimum %d", count, m.MinFields),
	}, nil
}

func parseJSONPathSteps(path string) ([]jsonPathStep, error) {
	parts := strings.Split(path, ".")
	steps := make([]jsonPathStep, 0, len(parts))

	for _, part := range parts {
		match := jsonPathSegmentRE.FindStringSubmatch(part)
		if match == nil {
			return nil, fmt.Errorf("unsupported JSONPath %q: supported syntax is dot-separated keys with optional [index], e.g. fields.adults or items[0].name", path)
		}

		step := jsonPathStep{key: match[1]}
		for _, idxMatch := range jsonPathIndexRE.FindAllStringSubmatch(match[2], -1) {
			idx, err := strconv.Atoi(idxMatch[1])
			if err != nil {
				return nil, fmt.Errorf("invalid JSONPath index %q", idxMatch[1])
			}
			step.indices = append(step.indices, idx)
		}

		steps = append(steps, step)
	}

	return steps, nil
}

func extractJSONPathValue(payload any, steps []jsonPathStep) (any, bool, string) {
	cur := payload

	for _, step := range steps {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false, fmt.Sprintf("path lookup failed at key %q: current node is not an object", step.key)
		}

		next, exists := obj[step.key]
		if !exists {
			return nil, false, fmt.Sprintf("path lookup failed: key %q not found", step.key)
		}

		cur = next
		for _, idx := range step.indices {
			arr, ok := cur.([]any)
			if !ok {
				return nil, false, fmt.Sprintf("path lookup failed at index [%d]: current node is not an array", idx)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, false, fmt.Sprintf("path lookup failed: index [%d] out of range", idx)
			}
			cur = arr[idx]
		}
	}

	return cur, true, ""
}

func stringifyJSONValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}

func decodeJSONAny(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()

	var payload any
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}

	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not supported")
		}
		return nil, err
	}
	return payload, nil
}
