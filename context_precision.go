package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/context_precision.tmpl
var contextPrecisionTmpl string

var contextPrecisionTemplate = template.Must(template.New("context_precision").Parse(contextPrecisionTmpl))

// ContextPrecision measures whether the retrieved documents in Case.Context
// are relevant to Case.Input.
type ContextPrecision struct {
	Threshold float64
}

// Name implements Metric.
func (m ContextPrecision) Name() string { return "ContextPrecision" }

// Score implements Metric.
func (m ContextPrecision) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	return runTemplateMetric(ctx, j, contextPrecisionTemplate, c, "context_precision", m.Name(), defaultFloat(m.Threshold, 0.7))
}
