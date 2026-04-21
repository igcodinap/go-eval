package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/faithfulness.tmpl
var faithfulnessTmpl string

var faithfulnessTemplate = template.Must(template.New("faithfulness").Parse(faithfulnessTmpl))

// Faithfulness measures whether Case.Output's factual claims are supported
// by Case.Context.
type Faithfulness struct {
	Threshold float64
}

// Name implements Metric.
func (m Faithfulness) Name() string { return "Faithfulness" }

// Score implements Metric.
func (m Faithfulness) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return runTemplateMetric(ctx, j, faithfulnessTemplate, c, "faithfulness", m.Name(), defaultFloat(m.Threshold, 0.8))
}
