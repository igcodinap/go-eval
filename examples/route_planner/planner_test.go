package main

import (
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestRoutePlannerArtifacts(t *testing.T) {
	r := eval.NewRunner(nil)

	c := eval.Case{
		Input: "Plan a prepaid route from Universidad de Santiago to Valparaiso.",
		Metadata: map[string]any{
			"case_id": "route-usach-valparaiso-card",
			"flow":    "route_planner.plan",
			"tier":    "critical",
			"dataset": "route-planner/smoke-v1",
		},
	}

	output, artifacts, err := planRoute(c.Input)
	if err != nil {
		t.Fatalf("planRoute: %v", err)
	}
	c.Output = output
	c.Artifacts = artifacts

	r.Run(t, eval.Contains{}, eval.Case{
		Output:   c.Output,
		Expected: "Estimated time",
		Metadata: c.Metadata,
	})
	r.Run(t, eval.ArtifactExists{Key: "route"}, c)
	r.Run(t, eval.ArtifactJSONPath{Key: "route", Path: "status", Expected: "ready"}, c)
	r.Run(t, eval.ArtifactJSONPath{Key: "state", Path: "payment.ready", Expected: "true"}, c)
	r.Run(t, eval.ArtifactArrayContains{Key: "route", Path: "stops", Expected: "Pajaritos"}, c)
	r.Run(t, eval.ArtifactNumberLTE{Key: "route", Path: "total_minutes", Max: 120}, c)
	r.Run(t, eval.ArtifactNumberLTE{Key: "budget", Path: "tokens", Max: 800}, c)
}
