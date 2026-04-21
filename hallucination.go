package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/hallucination.tmpl
var hallucinationTmpl string

var hallucinationTemplate = template.Must(template.New("hallucination").Parse(hallucinationTmpl))

// Hallucination measures whether Case.Output invents facts not present in
// Case.Context. Higher scores mean fewer hallucinations.
type Hallucination struct {
	Threshold float64
}

// Name implements Metric.
func (m Hallucination) Name() string { return "Hallucination" }

// Score implements Metric.
func (m Hallucination) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return runTemplateMetric(ctx, j, hallucinationTemplate, c, "hallucination", m.Name(), defaultFloat(m.Threshold, 0.9))
}
