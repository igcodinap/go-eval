package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"text/template"
	"time"
)

func defaultFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func renderPrompt(tmpl *template.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runTemplateMetric(
	ctx context.Context,
	j Judge,
	tmpl *template.Template,
	data any,
	metricKey string,
	metricName string,
	threshold float64,
) (Result, error) {
	prompt, err := renderPrompt(tmpl, data)
	if err != nil {
		return Result{Metric: metricName}, fmt.Errorf("%s: render prompt: %w", metricKey, err)
	}
	return runPromptMetric(ctx, j, prompt, metricKey, metricName, threshold)
}

func runPromptMetric(
	ctx context.Context,
	j Judge,
	prompt string,
	metricKey string,
	metricName string,
	threshold float64,
) (Result, error) {
	if j == nil {
		return Result{Metric: metricName}, fmt.Errorf("%s: %w", metricKey, errors.New("nil judge"))
	}

	start := time.Now()
	resp, err := j.Evaluate(ctx, prompt)
	latency := time.Since(start)
	if err != nil {
		return Result{Metric: metricName, Latency: latency}, fmt.Errorf("%s: judge: %w", metricKey, err)
	}

	return Result{
		Score:            resp.Score,
		Reason:           resp.Reason,
		Passed:           resp.Score >= threshold,
		Metric:           metricName,
		Latency:          latency,
		Tokens:           resp.Tokens,
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
	}, nil
}
