package eval

import (
	"context"
	_ "embed"
	"text/template"
)

//go:embed prompts/geval.tmpl
var gevalTmpl string

var gevalFuncs = template.FuncMap{
	"add": func(a, b int) int { return a + b },
}

var gevalTemplate = template.Must(template.New("geval").Funcs(gevalFuncs).Parse(gevalTmpl))

// GEval is a custom LLM-as-judge metric driven by a user-supplied rubric.
type GEval struct {
	Criteria  string
	Steps     []string
	Threshold float64
}

// Name implements Metric.
func (m GEval) Name() string { return "GEval" }

// Score implements Metric.
func (m GEval) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	data := struct {
		Case
		Criteria string
		Steps    []string
	}{
		Case:     c,
		Criteria: m.Criteria,
		Steps:    m.Steps,
	}

	return runTemplateMetric(ctx, j, gevalTemplate, data, "geval", m.Name(), defaultFloat(m.Threshold, 0.7))
}
