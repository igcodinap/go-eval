package eval

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/compound.tmpl
var compoundTmpl string

var compoundTemplate = template.Must(template.New("compound").Parse(compoundTmpl))

// Dimension is one scoring criterion inside a Compound metric.
type Dimension struct {
	Name      string
	Rubric    string
	Threshold float64
}

// Compound scores multiple dimensions in one judge call.
type Compound struct {
	Dimensions []Dimension
}

// Name implements Metric.
func (m Compound) Name() string { return "Compound" }

// Score implements Metric.
func (m Compound) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	dimensions, err := validateDimensions(m.Dimensions)
	if err != nil {
		return Result{Metric: m.Name()}, fmt.Errorf("compound: %w", err)
	}

	rawJudge, ok := j.(RawJudge)
	if !ok {
		return Result{Metric: m.Name()}, errors.New("compound requires a judge implementing RawJudge")
	}

	prompt, err := renderPrompt(compoundTemplate, struct {
		Case
		Dimensions []Dimension
	}{
		Case:       c,
		Dimensions: dimensions,
	})
	if err != nil {
		return Result{Metric: m.Name()}, fmt.Errorf("compound: render prompt: %w", err)
	}

	start := time.Now()
	totalTokens := 0

	attemptPrompt := prompt
	const maxAttempts = 2
	var lastParseErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		rawResp, evalErr := rawJudge.EvaluateRaw(ctx, attemptPrompt)
		totalTokens += rawResp.Tokens
		if evalErr != nil {
			return Result{Metric: m.Name(), Latency: time.Since(start), Tokens: totalTokens}, fmt.Errorf("compound: judge: %w", evalErr)
		}

		dimensionResults, parseErr := parseCompoundDimensions(rawResp.Content, dimensions)
		if parseErr == nil {
			return buildCompoundResult(m.Name(), dimensionResults, time.Since(start), totalTokens), nil
		}
		lastParseErr = parseErr

		if attempt < maxAttempts-1 {
			attemptPrompt = prompt + "\n\nJSON only, no prose."
			continue
		}
	}
	if lastParseErr != nil {
		return Result{Metric: m.Name(), Latency: time.Since(start), Tokens: totalTokens}, fmt.Errorf("compound: parse response: %w", lastParseErr)
	}
	return Result{Metric: m.Name(), Latency: time.Since(start), Tokens: totalTokens}, errors.New("compound: exhausted retry budget")
}

func validateDimensions(input []Dimension) ([]Dimension, error) {
	if len(input) == 0 {
		return nil, errors.New("at least one dimension is required")
	}

	seen := make(map[string]struct{}, len(input))
	out := make([]Dimension, len(input))

	for i, d := range input {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return nil, fmt.Errorf("dimension[%d] name is required", i)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate dimension name %q", name)
		}
		seen[name] = struct{}{}

		if math.IsNaN(d.Threshold) || d.Threshold < 0 || d.Threshold > 1 {
			return nil, fmt.Errorf("dimension %q threshold %.4f is outside [0,1]", name, d.Threshold)
		}
		if strings.TrimSpace(d.Rubric) == "" {
			return nil, fmt.Errorf("dimension %q rubric is required", name)
		}

		out[i] = Dimension{
			Name:      name,
			Rubric:    d.Rubric,
			Threshold: d.Threshold,
		}
	}

	return out, nil
}

func parseCompoundDimensions(content string, dimensions []Dimension) ([]DimensionResult, error) {
	candidate := ExtractJSONObjectCandidate(content)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}

	results := make([]DimensionResult, 0, len(dimensions))
	for _, dim := range dimensions {
		rawValue, ok := payload[dim.Name]
		if !ok {
			return nil, fmt.Errorf("missing dimension %q in response", dim.Name)
		}

		var nested struct {
			Score  *float64 `json:"score"`
			Reason string   `json:"reason"`
		}
		if err := json.Unmarshal(rawValue, &nested); err != nil {
			return nil, fmt.Errorf("dimension %q parse failed: %w; expected object {\"score\": number, \"reason\": string} (flat format not supported)", dim.Name, err)
		}
		if nested.Score == nil {
			return nil, fmt.Errorf("dimension %q missing score", dim.Name)
		}
		if *nested.Score < 0 || *nested.Score > 1 {
			return nil, fmt.Errorf("dimension %q score %.4f is outside [0,1]", dim.Name, *nested.Score)
		}

		passed := true
		if dim.Threshold > 0 {
			passed = *nested.Score >= dim.Threshold
		}

		results = append(results, DimensionResult{
			Name:      dim.Name,
			Score:     *nested.Score,
			Threshold: dim.Threshold,
			Passed:    passed,
			Reason:    nested.Reason,
		})
	}

	return results, nil
}

func buildCompoundResult(metric string, dimensions []DimensionResult, latency time.Duration, tokens int) Result {
	var sum float64
	allPassed := true
	parts := make([]string, 0, len(dimensions))

	for _, dim := range dimensions {
		sum += dim.Score
		if dim.Threshold > 0 && !dim.Passed {
			allPassed = false
			parts = append(parts, fmt.Sprintf("%s=%.2f(<%.2f)", dim.Name, dim.Score, dim.Threshold))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%.2f", dim.Name, dim.Score))
	}

	return Result{
		Score:      sum / float64(len(dimensions)),
		Reason:     strings.Join(parts, " "),
		Passed:     allPassed,
		Metric:     metric,
		Latency:    latency,
		Tokens:     tokens,
		Dimensions: dimensions,
	}
}
