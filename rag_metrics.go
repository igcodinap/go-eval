package eval

import (
	"context"
	"strings"
	"text/template"
)

var contextRecallTemplate = template.Must(template.New("context_recall").Parse(`You are grading retrieved context recall.

Score from 0 to 1.
Return JSON with fields "score" and "reason".

Question:
{{.Input}}

Expected answer or facts:
{{.Expected}}

Retrieved context:
{{.Context}}

Rubric:
{{.Rubric}}
`))

var answerCorrectnessTemplate = template.Must(template.New("answer_correctness").Parse(`You are grading answer correctness.

Score from 0 to 1.
Return JSON with fields "score" and "reason".

Question:
{{.Input}}

Expected answer:
{{.Expected}}

Actual answer:
{{.Output}}

Rubric:
{{.Rubric}}
`))

var noiseSensitivityTemplate = template.Must(template.New("noise_sensitivity").Parse(`You are grading whether an answer stayed robust in the presence of distracting or irrelevant context.

Score from 0 to 1.
Return JSON with fields "score" and "reason".

Question:
{{.Input}}

Expected answer:
{{.Expected}}

Actual answer:
{{.Output}}

Retrieved context, including any distractors:
{{.Context}}

Rubric:
{{.Rubric}}
`))

// ContextRecall measures whether the retrieved context contains the expected
// answer or facts needed to answer the input.
type ContextRecall struct {
	Threshold float64
	Rubric    string
}

// Name implements Metric.
func (m ContextRecall) Name() string { return "ContextRecall" }

// Score implements Metric.
func (m ContextRecall) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if strings.TrimSpace(c.Expected) == "" {
		return failedRAGResult(m.Name(), "expected answer or facts are empty"), nil
	}
	if len(c.Context) == 0 {
		return failedRAGResult(m.Name(), "context is empty"), nil
	}
	rubric := strings.TrimSpace(m.Rubric)
	if rubric == "" {
		rubric = "High scores require the context to contain the facts needed to produce the expected answer."
	}
	return runTemplateMetric(ctx, j, contextRecallTemplate, struct {
		Input    string
		Expected string
		Context  string
		Rubric   string
	}{
		Input:    c.Input,
		Expected: c.Expected,
		Context:  strings.Join(c.Context, "\n---\n"),
		Rubric:   rubric,
	}, "context_recall", m.Name(), defaultFloat(m.Threshold, 0.7))
}

// AnswerCorrectness measures whether the output matches the expected answer.
type AnswerCorrectness struct {
	Threshold float64
	Rubric    string
}

// Name implements Metric.
func (m AnswerCorrectness) Name() string { return "AnswerCorrectness" }

// Score implements Metric.
func (m AnswerCorrectness) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if strings.TrimSpace(c.Expected) == "" {
		return failedRAGResult(m.Name(), "expected answer is empty"), nil
	}
	rubric := strings.TrimSpace(m.Rubric)
	if rubric == "" {
		rubric = "High scores require the actual answer to be semantically equivalent to the expected answer."
	}
	return runTemplateMetric(ctx, j, answerCorrectnessTemplate, struct {
		Input    string
		Expected string
		Output   string
		Rubric   string
	}{
		Input:    c.Input,
		Expected: c.Expected,
		Output:   c.Output,
		Rubric:   rubric,
	}, "answer_correctness", m.Name(), defaultFloat(m.Threshold, 0.7))
}

// NoiseSensitivity measures whether the output avoids being misled by
// irrelevant or distracting retrieved context.
type NoiseSensitivity struct {
	Threshold float64
	Rubric    string
}

// Name implements Metric.
func (m NoiseSensitivity) Name() string { return "NoiseSensitivity" }

// Score implements Metric.
func (m NoiseSensitivity) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if len(c.Context) == 0 {
		return failedRAGResult(m.Name(), "context is empty"), nil
	}
	rubric := strings.TrimSpace(m.Rubric)
	if rubric == "" {
		rubric = "High scores require the answer to rely on relevant context and ignore distractors or conflicting irrelevant context."
	}
	return runTemplateMetric(ctx, j, noiseSensitivityTemplate, struct {
		Input    string
		Expected string
		Output   string
		Context  string
		Rubric   string
	}{
		Input:    c.Input,
		Expected: c.Expected,
		Output:   c.Output,
		Context:  strings.Join(c.Context, "\n---\n"),
		Rubric:   rubric,
	}, "noise_sensitivity", m.Name(), defaultFloat(m.Threshold, 0.7))
}

func failedRAGResult(metricName string, reason string) Result {
	return Result{
		Score:  0,
		Passed: false,
		Metric: metricName,
		Reason: reason,
	}
}
