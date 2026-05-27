package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContractPassesWithDimensions(t *testing.T) {
	result, err := (Contract{
		ContractName: "route_ready",
		Checks: []Metric{
			scriptedMetric{name: "A", result: Result{Score: 1, Passed: true, Metric: "A", Reason: "ok"}},
			scriptedMetric{name: "B", result: Result{Score: 0.8, Passed: true, Metric: "B", Reason: "ok"}},
		},
	}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Metric != "Contract(route_ready)" || len(result.Dimensions) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestContractCollectsFailuresByDefault(t *testing.T) {
	var calls int
	result, err := (Contract{
		ContractName: "route_ready",
		Checks: []Metric{
			funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
				calls++
				return Result{Score: 0, Passed: false, Metric: "A", Reason: "bad"}, nil
			}),
			funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
				calls++
				return Result{Score: 1, Passed: true, Metric: "B", Reason: "ok"}, nil
			}),
		},
	}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || calls != 2 || len(result.Dimensions) != 2 || !strings.Contains(result.Reason, "A") {
		t.Fatalf("unexpected result: %+v calls=%d", result, calls)
	}
}

func TestContractStopOnFailure(t *testing.T) {
	var calls int
	result, err := (Contract{
		ContractName:  "route_ready",
		StopOnFailure: true,
		Checks: []Metric{
			funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
				calls++
				return Result{Score: 0, Passed: false, Metric: "A"}, nil
			}),
			funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
				calls++
				return Result{Score: 1, Passed: true, Metric: "B"}, nil
			}),
		},
	}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || calls != 1 || len(result.Dimensions) != 1 {
		t.Fatalf("unexpected result: %+v calls=%d", result, calls)
	}
}

func TestContractValidationErrors(t *testing.T) {
	if _, err := (Contract{}).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected empty name error")
	}
	if _, err := (Contract{ContractName: "x", Checks: []Metric{nil}}).Score(context.Background(), nil, Case{}); err == nil {
		t.Fatalf("expected nil check error")
	}
	wantErr := errors.New("judge")
	if _, err := (Contract{
		ContractName: "x",
		Checks:       []Metric{scriptedMetric{name: "X", err: wantErr}},
	}).Score(context.Background(), nil, Case{}); !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped metric error, got %v", err)
	}
}
