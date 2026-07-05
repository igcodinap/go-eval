package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Rubric is a named, versioned custom LLM-as-judge metric.
//
// It uses the same prompt shape as GEval but gives teams a stable metric name
// and metadata for comparing custom rubrics across releases.
type Rubric struct {
	ID        string
	Version   string
	Criteria  string
	Steps     []string
	Threshold float64
	_         struct{}
}

// Name implements Metric.
func (m Rubric) Name() string {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return "Rubric"
	}
	return "Rubric(" + id + ")"
}

// Score implements Metric.
func (m Rubric) Score(ctx context.Context, j Judge, c Case) (Result, error) {
	if strings.TrimSpace(m.ID) == "" {
		return Result{Metric: m.Name()}, errors.New("rubric: ID is required")
	}
	if strings.TrimSpace(m.Criteria) == "" {
		return Result{Metric: m.Name()}, errors.New("rubric: Criteria is required")
	}

	data := struct {
		Case
		Criteria string
		Steps    []string
	}{
		Case:     c,
		Criteria: m.Criteria,
		Steps:    m.Steps,
	}

	result, err := runTemplateMetric(ctx, j, gevalTemplate, data, "rubric", m.Name(), defaultFloat(m.Threshold, 0.7))
	if result.Metadata == nil {
		result.Metadata = cloneMetadata(c.Metadata)
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["rubric_id"] = m.ID
	if m.Version != "" {
		result.Metadata["rubric_version"] = m.Version
	}
	if err != nil {
		return result, fmt.Errorf("rubric %q: %w", m.ID, err)
	}
	return result, nil
}
