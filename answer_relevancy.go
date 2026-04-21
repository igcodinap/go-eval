package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/answer_relevancy.tmpl
var answerRelevancyTmpl string

var answerRelevancyTemplate = template.Must(template.New("answer_relevancy").Parse(answerRelevancyTmpl))

// AnswerRelevancy measures whether Case.Output actually addresses Case.Input.
type AnswerRelevancy struct {
	Threshold    float64
	NumQuestions int
}

// Name implements Metric.
func (m AnswerRelevancy) Name() string { return "AnswerRelevancy" }

// Score implements Metric.
func (m AnswerRelevancy) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	data := struct {
		Case
		NumQuestions int
	}{
		Case:         c,
		NumQuestions: defaultInt(m.NumQuestions, 3),
	}

	return runTemplateMetric(ctx, j, answerRelevancyTemplate, data, "answer_relevancy", m.Name(), defaultFloat(m.Threshold, 0.7))
}
