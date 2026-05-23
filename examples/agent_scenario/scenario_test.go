package agent_scenario

import (
	"context"
	"encoding/json"
	"testing"

	eval "github.com/igcodinap/go-eval"
)

func TestRoutePlanningScenario(t *testing.T) {
	t.Setenv(eval.EnvVar, "1")

	r := eval.NewRunner(
		nil,
		eval.WithResultSink(eval.DefaultResultSink()),
		eval.WithRedactors(
			eval.UUIDRedactor(),
			eval.FieldRedactor("trip_plan_id"),
		),
	)

	result := r.RunScenario(t, eval.Scenario{
		Name: "planning_to_route_ready",
		Tier: "critical",
		Metadata: map[string]any{
			"flow":    "route_planner.plan",
			"dataset": "route-planner/scenario-v1",
		},
		Tools:  eval.NewToolRegistry("plan_route", "select_map_items"),
		Driver: routePlannerDriver,
		Steps: []eval.Step{
			{
				Name:           "greeting",
				Input:          "Hola",
				ForbiddenTools: []string{"plan_route", "select_map_items"},
				MaxToolCalls:   1,
			},
			{
				Name:          "ready_route_request",
				Input:         "Propón la ruta",
				RequiredTools: []string{"plan_route"},
				Checks: []eval.Metric{
					eval.ArtifactJSONPath{Key: "trip_plan", Path: "status", Expected: "ready"},
					eval.ArtifactJSONPath{Key: "route", Path: "success", Expected: "true"},
					eval.ArtifactArrayMinLen{Key: "route", Path: "stops", MinLen: 2},
				},
				Metadata: map[string]any{
					"trip_plan_id": "550e8400-e29b-41d4-a716-446655440000",
				},
			},
		},
	})
	if !result.Passed {
		t.Fatalf("scenario failed: %+v", result.Results)
	}
}

func routePlannerDriver(ctx context.Context, req eval.StepRequest) (eval.StepResult, error) {
	_ = ctx

	switch req.Step.Name {
	case "greeting":
		return eval.StepResult{
			Output: "Hola, puedo ayudarte a planificar una ruta.",
			Turns: []eval.Turn{
				{Role: eval.RoleUser, Content: req.Step.Input},
				{Role: eval.RoleAssistant, Content: "Hola, puedo ayudarte a planificar una ruta."},
			},
		}, nil
	case "ready_route_request":
		return eval.StepResult{
			Output: "Ruta lista: Universidad de Santiago -> Pajaritos -> Valparaiso.",
			Turns: []eval.Turn{
				{Role: eval.RoleUser, Content: req.Step.Input},
				{
					Role: eval.RoleAssistant,
					ToolCalls: []eval.ToolCall{
						{
							Name:      "plan_route",
							Arguments: json.RawMessage(`{"from":"Universidad de Santiago","to":"Valparaiso"}`),
							Result:    "route ready",
						},
					},
				},
			},
			Artifacts: map[string]json.RawMessage{
				"trip_plan": json.RawMessage(`{"status":"ready"}`),
				"route":     json.RawMessage(`{"success":true,"stops":["Universidad de Santiago","Pajaritos","Valparaiso"]}`),
			},
		}, nil
	default:
		return eval.StepResult{}, nil
	}
}
