package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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

// ArtifactNotExists checks whether a Case does not contain a named artifact.
type ArtifactNotExists struct {
	Key string
}

// Name implements Metric.
func (m ArtifactNotExists) Name() string { return "ArtifactNotExists" }

// Score implements Metric.
func (m ArtifactNotExists) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	key := strings.TrimSpace(m.Key)
	if key == "" {
		return failedArtifactResult(m.Name(), "artifact key is empty"), nil
	}
	if _, ok := c.Artifacts[key]; ok {
		return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q exists", key)), nil
	}
	return Result{
		Score:  1.0,
		Passed: true,
		Metric: m.Name(),
		Reason: fmt.Sprintf("artifact %q does not exist", key),
	}, nil
}

// ArtifactJSONPath compares a JSON path inside a named artifact to Expected.
//
// Expected is compared to the stringified JSON value: strings compare as-is,
// booleans compare as "true" or "false", numbers compare in JSON number form,
// and objects or arrays compare as compact JSON.
type ArtifactJSONPath struct {
	Key        string
	Path       string
	Expected   string
	Normalizer Normalizer
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

	expected := normalizeString(m.Normalizer, m.Expected)
	actual := stringifyJSONValue(value)
	if normalizeString(m.Normalizer, actual) == expected {
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
	Key        string
	Path       string
	Expected   string
	Normalizer Normalizer
}

// Name implements Metric.
func (m ArtifactArrayContains) Name() string { return "ArtifactArrayContains" }

// Score implements Metric.
func (m ArtifactArrayContains) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	items, ok, reason := artifactArrayItems(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	expected := normalizeString(m.Normalizer, m.Expected)
	for _, item := range items {
		if normalizeString(m.Normalizer, stringifyJSONValue(item)) == expected {
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

// ArtifactArrayNotContains checks whether an array artifact value excludes Expected.
//
// Expected uses the same stringified JSON value comparison as ArtifactJSONPath.
type ArtifactArrayNotContains struct {
	Key        string
	Path       string
	Expected   string
	Normalizer Normalizer
}

// Name implements Metric.
func (m ArtifactArrayNotContains) Name() string { return "ArtifactArrayNotContains" }

// Score implements Metric.
func (m ArtifactArrayNotContains) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	items, ok, reason := artifactArrayItems(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}

	expected := normalizeString(m.Normalizer, m.Expected)
	for _, item := range items {
		if normalizeString(m.Normalizer, stringifyJSONValue(item)) == expected {
			return failedArtifactResult(
				m.Name(),
				fmt.Sprintf("artifact %q path %q contains excluded value %q", m.Key, m.Path, m.Expected),
			), nil
		}
	}
	return Result{
		Score:  1.0,
		Passed: true,
		Metric: m.Name(),
		Reason: fmt.Sprintf("artifact %q path %q excludes value %q", m.Key, m.Path, m.Expected),
	}, nil
}

// ArtifactSubset checks whether an artifact value contains an expected JSON subset.
type ArtifactSubset struct {
	Key        string
	Path       string
	Expected   json.RawMessage
	Normalizer Normalizer
}

// Name implements Metric.
func (m ArtifactSubset) Name() string { return "ArtifactSubset" }

// Score implements Metric.
func (m ArtifactSubset) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx

	if len(strings.TrimSpace(string(m.Expected))) == 0 {
		return failedArtifactResult(m.Name(), "expected subset is empty"), nil
	}
	expected, err := decodeJSONAny(string(m.Expected))
	if err != nil {
		return failedArtifactResult(m.Name(), fmt.Sprintf("expected subset is not valid JSON: %v", err)), nil
	}
	values, ok, reason := artifactPathValues(c, m.Key, m.Path)
	if !ok {
		return failedArtifactResult(m.Name(), reason), nil
	}
	for _, value := range values {
		if ok, reason := jsonSubsetMatches(value, expected, m.Normalizer, ""); ok {
			return Result{
				Score:  1.0,
				Passed: true,
				Metric: m.Name(),
				Reason: fmt.Sprintf("artifact %q path %q contains expected subset", m.Key, m.Path),
			}, nil
		} else if len(values) == 1 {
			return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q subset mismatch: %s", m.Key, m.Path, reason)), nil
		}
	}
	return failedArtifactResult(m.Name(), fmt.Sprintf("artifact %q path %q does not contain expected subset", m.Key, m.Path)), nil
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

func artifactPathValues(c Case, key string, path string) ([]any, bool, string) {
	if !strings.Contains(path, "[*]") {
		value, ok, reason := artifactPathValue(c, key, path)
		if !ok {
			return nil, false, reason
		}
		return []any{value}, true, ""
	}

	payload, ok, reason := artifactValue(c, key)
	if !ok {
		return nil, false, reason
	}
	steps, err := parseWildcardJSONPathSteps(strings.TrimSpace(path))
	if err != nil {
		return nil, false, err.Error()
	}
	values, ok, reason := extractWildcardJSONPathValues(payload, steps)
	if !ok {
		return nil, false, reason
	}
	if len(values) == 0 {
		return nil, false, fmt.Sprintf("path %q matched no values", path)
	}
	return values, true, ""
}

func artifactArrayItems(c Case, key string, path string) ([]any, bool, string) {
	values, ok, reason := artifactPathValues(c, key, path)
	if !ok {
		return nil, false, reason
	}
	if strings.Contains(path, "[*]") {
		return values, true, ""
	}
	items, ok := values[0].([]any)
	if !ok {
		return nil, false, fmt.Sprintf("artifact %q path %q is not a JSON array", key, path)
	}
	return items, true, ""
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

var (
	wildcardJSONPathSegmentRE = regexp.MustCompile(`^([A-Za-z_]\w*)((?:\[(?:\d+|\*)\])*)$`)
	wildcardJSONPathIndexRE   = regexp.MustCompile(`\[(\d+|\*)\]`)
)

type wildcardJSONPathStep struct {
	key     string
	indices []wildcardJSONPathIndex
}

type wildcardJSONPathIndex struct {
	index    int
	wildcard bool
}

func parseWildcardJSONPathSteps(path string) ([]wildcardJSONPathStep, error) {
	if path == "" {
		return nil, nil
	}
	parts := strings.Split(path, ".")
	steps := make([]wildcardJSONPathStep, 0, len(parts))
	for _, part := range parts {
		match := wildcardJSONPathSegmentRE.FindStringSubmatch(part)
		if match == nil {
			return nil, fmt.Errorf("unsupported JSONPath %q: supported syntax is dot-separated keys with optional [index] or [*]", path)
		}
		step := wildcardJSONPathStep{key: match[1]}
		for _, idxMatch := range wildcardJSONPathIndexRE.FindAllStringSubmatch(match[2], -1) {
			if idxMatch[1] == "*" {
				step.indices = append(step.indices, wildcardJSONPathIndex{wildcard: true})
				continue
			}
			idx, err := strconv.Atoi(idxMatch[1])
			if err != nil {
				return nil, fmt.Errorf("invalid JSONPath index %q", idxMatch[1])
			}
			step.indices = append(step.indices, wildcardJSONPathIndex{index: idx})
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func extractWildcardJSONPathValues(payload any, steps []wildcardJSONPathStep) ([]any, bool, string) {
	current := []any{payload}
	for _, step := range steps {
		next := make([]any, 0, len(current))
		for _, node := range current {
			obj, ok := node.(map[string]any)
			if !ok {
				return nil, false, fmt.Sprintf("path lookup failed at key %q: current node is not an object", step.key)
			}
			value, exists := obj[step.key]
			if !exists {
				return nil, false, fmt.Sprintf("path lookup failed: key %q not found", step.key)
			}
			values, ok, reason := applyWildcardIndices(value, step.indices)
			if !ok {
				return nil, false, reason
			}
			next = append(next, values...)
		}
		current = next
	}
	return current, true, ""
}

func applyWildcardIndices(value any, indices []wildcardJSONPathIndex) ([]any, bool, string) {
	values := []any{value}
	for _, idx := range indices {
		next := []any{}
		for _, value := range values {
			arr, ok := value.([]any)
			if !ok {
				if idx.wildcard {
					return nil, false, "path lookup failed at index [*]: current node is not an array"
				}
				return nil, false, fmt.Sprintf("path lookup failed at index [%d]: current node is not an array", idx.index)
			}
			if idx.wildcard {
				next = append(next, arr...)
				continue
			}
			if idx.index < 0 || idx.index >= len(arr) {
				return nil, false, fmt.Sprintf("path lookup failed: index [%d] out of range", idx.index)
			}
			next = append(next, arr[idx.index])
		}
		values = next
	}
	return values, true, ""
}

func jsonSubsetMatches(actual any, expected any, normalizer Normalizer, path string) (bool, string) {
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return false, pathOrRoot(path) + " is not an object"
		}
		for key, expectedValue := range want {
			actualValue, exists := got[key]
			nextPath := joinJSONPath(path, key)
			if !exists {
				return false, nextPath + " is missing"
			}
			if ok, reason := jsonSubsetMatches(actualValue, expectedValue, normalizer, nextPath); !ok {
				return false, reason
			}
		}
		return true, ""
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return false, pathOrRoot(path) + " is not an array"
		}
		return jsonArraySubsetMatches(got, want, normalizer, path)
	case string:
		got, ok := actual.(string)
		if !ok {
			return false, pathOrRoot(path) + " is not a string"
		}
		if normalizeString(normalizer, got) != normalizeString(normalizer, want) {
			return false, fmt.Sprintf("%s mismatch: got %q, expected %q", pathOrRoot(path), got, want)
		}
		return true, ""
	default:
		if stringifyJSONValue(actual) != stringifyJSONValue(expected) {
			return false, fmt.Sprintf("%s mismatch: got %q, expected %q", pathOrRoot(path), stringifyJSONValue(actual), stringifyJSONValue(expected))
		}
		return true, ""
	}
}

func jsonArraySubsetMatches(actual []any, expected []any, normalizer Normalizer, path string) (bool, string) {
	if len(actual) < len(expected) {
		return false, fmt.Sprintf("%s length %d below expected %d", pathOrRoot(path), len(actual), len(expected))
	}

	used := make([]bool, len(actual))
	useMemo := len(actual) <= 64
	failedStates := map[arraySubsetSearchState]struct{}{}
	failureIndex := -1
	failureReason := ""
	var search func(int, uint64) bool
	search = func(expectedIndex int, usedMask uint64) bool {
		if expectedIndex == len(expected) {
			return true
		}
		var state arraySubsetSearchState
		if useMemo {
			state = arraySubsetSearchState{expectedIndex: expectedIndex, usedMask: usedMask}
			if _, failed := failedStates[state]; failed {
				return false
			}
		}
		localReason := ""
		for actualIndex, actualValue := range actual {
			if used[actualIndex] {
				continue
			}
			ok, reason := jsonSubsetMatches(actualValue, expected[expectedIndex], normalizer, fmt.Sprintf("%s[%d]", path, actualIndex))
			if !ok {
				if localReason == "" {
					localReason = reason
				}
				continue
			}
			used[actualIndex] = true
			nextMask := usedMask
			if useMemo {
				nextMask |= uint64(1) << uint(actualIndex)
			}
			if search(expectedIndex+1, nextMask) {
				return true
			}
			used[actualIndex] = false
		}
		if failureReason == "" {
			failureIndex = expectedIndex
			if localReason == "" {
				localReason = "no matching array element"
			}
			failureReason = localReason
		}
		if useMemo {
			failedStates[state] = struct{}{}
		}
		return false
	}
	if search(0, 0) {
		return true, ""
	}
	return false, fmt.Sprintf("%s expected element [%d] not found: %s", pathOrRoot(path), failureIndex, failureReason)
}

type arraySubsetSearchState struct {
	expectedIndex int
	usedMask      uint64
}

func joinJSONPath(path string, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func pathOrRoot(path string) string {
	if path == "" {
		return "$"
	}
	return path
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
