package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ArtifactExists checks whether a Case contains a named artifact.
type ArtifactExists struct {
	Key string
}

// Name implements Metric.
func (m ArtifactExists) Name() string { return "ArtifactExists" }

// Score implements Metric.
func (m ArtifactExists) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	key := strings.TrimSpace(m.Key)
	if key == "" {
		return failedArtifactResult(m.Name(), "artifact key is empty"), nil
	}
	if _, ok := c.Artifacts[key]; !ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q not found", key)), nil
	}
	return Result{
		Score:  1.0,
		Passed: true,
		Metric: m.Name(),
		Reason: fmt.Sprintf("artifact %q exists", key),
	}, nil
}

// ArtifactJSONPath compares a JSON path inside a named artifact to Expected.
//
// Expected is compared to the stringified JSON value: strings compare as-is,
// booleans compare as "true" or "false", numbers compare in JSON number form,
// and objects or arrays compare as compact JSON.
type ArtifactJSONPath struct {
	Key      string
	Path     string
	Expected string
}

// Name implements Metric.
func (m ArtifactJSONPath) Name() string { return "ArtifactJSONPath" }

// Score implements Metric.
func (m ArtifactJSONPath) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	value, ok, reason := artifactPathValue(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	actual := stringifyJSONValue(value)
	if actual == m.Expected {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("artifact %q path %q matched expected value", m.Key, m.Path),
		}, nil
	}
	return failedArtifactResult(
		m.Name(),
		fmt.Sprintf("artifact %q path %q mismatch: got %q, expected %q", m.Key, m.Path, actual, m.Expected),
	), nil
}

// ArtifactFieldCount checks the minimum non-null field count in an artifact object.
//
// Only JSON null is skipped. Empty strings, zero numbers, false booleans, empty
// arrays, and empty objects count as present fields. Scores are count/MinFields,
// capped at 1.0.
type ArtifactFieldCount struct {
	Key       string
	Path      string
	MinFields int
}

// Name implements Metric.
func (m ArtifactFieldCount) Name() string { return "ArtifactFieldCount" }

// Score implements Metric.
func (m ArtifactFieldCount) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if m.MinFields <= 0 {
		return failedArtifactResult(m.Name(), fmt.Sprintf("MinFields must be >= 1, got %d", m.MinFields)), nil
	}

	value, ok, reason := artifactPathValue(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	payload, ok := value.(map[string]any)
	if !ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q is not a JSON object", m.Key, m.Path)), nil
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
	if count >= m.MinFields {
		return Result{
			Score:  score,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("artifact %q non-null field count %d meets minimum %d", m.Key, count, m.MinFields),
		}, nil
	}
	return Result{
		Score:  score,
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("artifact %q non-null field count %d below minimum %d", m.Key, count, m.MinFields),
	}, nil
}

// ArtifactNumberLTE checks that a numeric artifact value is less than or equal to Max.
type ArtifactNumberLTE struct {
	Key  string
	Path string
	Max  float64
}

// Name implements Metric.
func (m ArtifactNumberLTE) Name() string { return "ArtifactNumberLTE" }

// Score implements Metric.
func (m ArtifactNumberLTE) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	value, ok, reason := artifactPathValue(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	actual, ok := numberAsFloat64(value)
	if !ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q is not a JSON number", m.Key, m.Path)), nil
	}
	if actual <= m.Max {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("artifact %q path %q value %g <= %g", m.Key, m.Path, actual, m.Max),
		}, nil
	}
	return failedArtifactResult(
		m.Name(),
		fmt.Sprintf("artifact %q path %q value %g > %g", m.Key, m.Path, actual, m.Max),
	), nil
}

// ArtifactArrayContains checks whether an array artifact value contains Expected.
//
// Expected uses the same stringified JSON value comparison as ArtifactJSONPath.
type ArtifactArrayContains struct {
	Key      string
	Path     string
	Expected string
}

// Name implements Metric.
func (m ArtifactArrayContains) Name() string { return "ArtifactArrayContains" }

// Score implements Metric.
func (m ArtifactArrayContains) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	value, ok, reason := artifactPathValue(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	items, ok := value.([]any)
	if !ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q is not a JSON array", m.Key, m.Path)), nil
	}
	for _, item := range items {
		if stringifyJSONValue(item) == m.Expected {
			return Result{
				Score:  1.0,
				Passed: true,
				Metric: m.Name(),
				Reason: fmt.Sprintf("artifact %q path %q contains expected value", m.Key, m.Path),
			}, nil
		}
	}
	return failedArtifactResult(
		m.Name(),
		fmt.Sprintf("artifact %q path %q does not contain expected value %q", m.Key, m.Path, m.Expected),
	), nil
}

// ArtifactArrayMinLen checks whether an array artifact value has at least MinLen items.
type ArtifactArrayMinLen struct {
	Key    string
	Path   string
	MinLen int
}

// Name implements Metric.
func (m ArtifactArrayMinLen) Name() string { return "ArtifactArrayMinLen" }

// Score implements Metric.
func (m ArtifactArrayMinLen) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if m.MinLen <= 0 {
		return failedArtifactResult(m.Name(), fmt.Sprintf("MinLen must be >= 1, got %d", m.MinLen)), nil
	}

	value, ok, reason := artifactPathValue(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	items, ok := value.([]any)
	if !ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q is not a JSON array", m.Key, m.Path)), nil
	}
	if len(items) >= m.MinLen {
		return Result{
			Score:  1.0,
			Passed: true,
			Metric: m.Name(),
			Reason: fmt.Sprintf("artifact %q path %q array length %d meets minimum %d", m.Key, m.Path, len(items), m.MinLen),
		}, nil
	}
	return Result{
		Score:  safeRatio(len(items), m.MinLen),
		Passed: false,
		Metric: m.Name(),
		Reason: fmt.Sprintf("artifact %q path %q array length %d below minimum %d", m.Key, m.Path, len(items), m.MinLen),
	}, nil
}

func artifactPathValue(c Case, key string, path string) (any, bool, string) {
	payload, ok, reason := artifactValue(c, key)
	if !ok {
		return nil, false, reason
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return payload, true, ""
	}

	steps, err := parseJSONPathSteps(path)
	if err != nil {
		return nil, false, err.Error()
	}
	value, ok, reason := extractJSONPathValue(payload, steps)
	if !ok {
		return nil, false, reason
	}
	return value, true, ""
}

func artifactValue(c Case, key string) (any, bool, string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, "artifact key is empty"
	}

	raw, ok := c.Artifacts[key]
	if !ok {
		return nil, false, fmt.Sprintf("artifact %q not found", key)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, false, fmt.Sprintf("artifact %q is empty", key)
	}

	payload, err := decodeJSONAny(string(raw))
	if err != nil {
		return nil, false, fmt.Sprintf("artifact %q is not valid JSON: %v", key, err)
	}
	return payload, true, ""
}

func numberAsFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case float64:
		return x, true
	default:
		return 0, false
	}
}

func failedArtifactResult(metric string, reason string) Result {
	return Result{
		Score:  0.0,
		Passed: false,
		Metric: metric,
		Reason: reason,
	}
}
