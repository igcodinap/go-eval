package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDecodeScenariosLoadsPortableDefinitions(t *testing.T) {
	scenarios, err := DecodeScenarios(strings.NewReader(`{
		"scenarios": [{
			"name": "checkout",
			"tier": "critical",
			"driver": "fake_checkout",
			"metadata": {"flow": "checkout.pay"},
			"state": {"cart_id": "c1"},
			"tools": ["charge", "receipt"],
			"repeat": {"n": 2, "pass_rate": 0.5},
			"steps": [{
				"name": "pay",
				"input": "pay now",
				"required_tools": ["charge"],
				"forbidden_tools": ["receipt"],
				"required_artifacts": ["payment"],
				"forbidden_artifacts": ["refund"],
				"max_tool_calls": 2,
				"metadata": {"case_id": "pay-1"},
				"timeout": "2s"
			}]
		}]
	}`))
	if err != nil {
		t.Fatalf("DecodeScenarios: %v", err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("expected one scenario, got %d", len(scenarios))
	}
	got := scenarios[0]
	if got.Name != "checkout" || got.DriverName != "fake_checkout" || got.Tier != "critical" {
		t.Fatalf("unexpected scenario: %+v", got)
	}
	if got.Repeat.N != 2 || got.Repeat.PassRate != 0.5 {
		t.Fatalf("repeat not preserved: %+v", got.Repeat)
	}
	step := got.Steps[0]
	if step.Timeout != 2*time.Second || step.RequiredArtifacts[0] != "payment" || step.ForbiddenArtifacts[0] != "refund" {
		t.Fatalf("step fields not preserved: %+v", step)
	}
}

func TestDecodeScenariosRejectsInvalidContracts(t *testing.T) {
	_, err := DecodeScenarios(strings.NewReader(`[
		{
			"name": "bad",
			"driver": "driver",
			"steps": [{"name": "one", "required_tool_patterns": ["route_["]}]
		}
	]`))
	if err == nil || !strings.Contains(err.Error(), "invalid tool pattern") {
		t.Fatalf("expected invalid pattern error, got %v", err)
	}
}

func TestDecodeScenariosRequiresDriverName(t *testing.T) {
	_, err := DecodeScenarios(strings.NewReader(`{"scenarios":[{"name":"missing","steps":[{"name":"one"}]}]}`))
	if err == nil || !strings.Contains(err.Error(), "driver is required") {
		t.Fatalf("expected driver error, got %v", err)
	}
}

func TestBindScenarioDriversAttachesDrivers(t *testing.T) {
	scenarios, err := DecodeScenarios(strings.NewReader(`{
		"scenarios": [{
			"name": "checkout",
			"driver": "fake_checkout",
			"steps": [{"name": "pay"}]
		}]
	}`))
	if err != nil {
		t.Fatalf("DecodeScenarios: %v", err)
	}
	bound, err := BindScenarioDrivers(scenarios, map[string]StepFunc{
		"fake_checkout": func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{Output: req.Step.Name}, nil
		},
	})
	if err != nil {
		t.Fatalf("BindScenarioDrivers: %v", err)
	}
	if bound[0].Driver == nil {
		t.Fatalf("driver was not attached")
	}
	result, err := bound[0].Driver(context.Background(), StepRequest{Step: bound[0].Steps[0]})
	if err != nil || result.Output != "pay" {
		t.Fatalf("unexpected driver result: %+v err=%v", result, err)
	}
}

func TestBindScenarioDriversReportsUnknownDriver(t *testing.T) {
	_, err := BindScenarioDrivers([]Scenario{{Name: "checkout", DriverName: "missing"}}, map[string]StepFunc{})
	if err == nil || !strings.Contains(err.Error(), `driver "missing" not found`) {
		t.Fatalf("expected unknown driver error, got %v", err)
	}
}
