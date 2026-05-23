package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// Scenario describes a sequential multi-step agent evaluation.
type Scenario struct {
	Name     string
	Tier     string
	Metadata map[string]any
	Tools    ToolRegistry
	Driver   StepFunc
	Steps    []Step
}

// Step describes one user interaction and its expected agent behavior.
type Step struct {
	Name           string
	Input          string
	RequiredTools  []string
	ForbiddenTools []string
	MaxToolCalls   int
	ExpectFail     bool
	Checks         []Metric
	Metadata       map[string]any
}

// StepRequest is passed to a Scenario driver for each step.
type StepRequest struct {
	Step      Step
	History   []Turn
	Artifacts map[string]json.RawMessage
}

// StepFunc runs one scenario step and returns the newly observed outputs.
type StepFunc func(ctx context.Context, req StepRequest) (StepResult, error)

// StepResult is the observed result of running one scenario step.
type StepResult struct {
	Output    string
	Turns     []Turn
	Artifacts map[string]json.RawMessage
	Metadata  map[string]any
}

// ScenarioResult is the aggregate result returned by RunScenario.
type ScenarioResult struct {
	Passed    bool
	Results   []Result
	Turns     []Turn
	Artifacts map[string]json.RawMessage
}

// RunScenario executes a sequential agent scenario and asserts via tb.
func (r *Runner) RunScenario(tb testing.TB, s Scenario) ScenarioResult {
	tb.Helper()

	if os.Getenv(EnvVar) == "" {
		tb.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return ScenarioResult{}
	}

	filterCase := Case{Metadata: scenarioBaseMetadata(s)}
	if r.caseFilter != nil && !r.caseFilter(filterCase) {
		tb.Skip("eval skipped by case filter")
		return ScenarioResult{}
	}

	if err := validateScenario(s); err != nil {
		tb.Fatalf("scenario %q: %v", s.Name, err)
		return ScenarioResult{}
	}

	out := ScenarioResult{Passed: true}
	var history []Turn
	artifacts := map[string]json.RawMessage{}

	for _, step := range s.Steps {
		stepResult, ok := r.runScenarioStep(tb, s, step, history, artifacts)
		if !ok {
			return out
		}

		stepTurns := cloneTurns(stepResult.Turns)
		history = append(history, stepTurns...)
		mergeArtifacts(artifacts, stepResult.Artifacts)

		stepMetadata := scenarioStepMetadata(s, step, stepResult.Metadata)
		stepCase := Case{
			Input:     step.Input,
			Output:    stepResult.Output,
			Turns:     cloneTurns(stepTurns),
			Metadata:  stepMetadata,
			Artifacts: cloneArtifacts(artifacts),
		}

		rawResults, ok := r.scoreScenarioStep(tb, s, step, stepCase)
		if !ok {
			return out
		}

		results, stepPassed := applyExpectFail(step, rawResults, stepMetadata)
		out.Passed = out.Passed && stepPassed
		out.Results = append(out.Results, results...)
		testName := scenarioStepTestName(tb.Name(), s.Name, step.Name)
		for _, result := range results {
			r.assertScenarioResult(tb, result)
			r.writeResultNamed(tb, testName, result)
		}
	}

	out.Turns = cloneTurns(history)
	out.Artifacts = cloneArtifacts(artifacts)
	return out
}

func validateScenario(s Scenario) error {
	if s.Name == "" {
		return errors.New("name is required")
	}
	if s.Driver == nil {
		return errors.New("driver is required")
	}
	if err := s.Tools.Validate(); err != nil {
		return fmt.Errorf("tool registry: %w", err)
	}

	seenSteps := make(map[string]struct{}, len(s.Steps))
	for i, step := range s.Steps {
		if step.Name == "" {
			return fmt.Errorf("step %d: name is required", i+1)
		}
		if _, exists := seenSteps[step.Name]; exists {
			return fmt.Errorf("step %q is duplicated", step.Name)
		}
		seenSteps[step.Name] = struct{}{}
		if err := validateScenarioToolNames(s.Tools, step); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		for j, check := range step.Checks {
			if check == nil {
				return fmt.Errorf("step %q: check %d is nil", step.Name, j+1)
			}
		}
	}
	return nil
}

func validateScenarioToolNames(registry ToolRegistry, step Step) error {
	if !registry.configured() {
		return nil
	}
	for _, name := range step.RequiredTools {
		if !registry.has(name) {
			return fmt.Errorf("required tool %q is not in registry", name)
		}
	}
	for _, name := range step.ForbiddenTools {
		if !registry.has(name) {
			return fmt.Errorf("forbidden tool %q is not in registry", name)
		}
	}
	return nil
}

func (r *Runner) runScenarioStep(
	tb testing.TB,
	s Scenario,
	step Step,
	history []Turn,
	artifacts map[string]json.RawMessage,
) (StepResult, bool) {
	tb.Helper()

	ctx, cancel := runnerContext(r.timeout)
	defer cancel()

	req := StepRequest{
		Step:      cloneStep(step),
		History:   cloneTurns(history),
		Artifacts: cloneArtifacts(artifacts),
	}
	stepResult, err := s.Driver(ctx, req)
	if err != nil {
		tb.Fatalf("scenario %q step %q: driver error: %v", s.Name, step.Name, err)
		return StepResult{}, false
	}
	return StepResult{
		Output:    stepResult.Output,
		Turns:     cloneTurns(stepResult.Turns),
		Artifacts: cloneArtifacts(stepResult.Artifacts),
		Metadata:  cloneMetadata(stepResult.Metadata),
	}, true
}

func (r *Runner) scoreScenarioStep(tb testing.TB, s Scenario, step Step, c Case) ([]Result, bool) {
	tb.Helper()

	metrics := scenarioStepMetrics(s.Tools, step)
	metrics = append(metrics, step.Checks...)

	results := make([]Result, 0, len(metrics))
	for _, metric := range metrics {
		result, err := r.scoreScenarioMetric(tb, metric, c)
		if err != nil {
			tb.Fatalf("%s: judge error: %v", metric.Name(), err)
			return nil, false
		}
		results = append(results, result)
	}
	return results, true
}

func scenarioStepMetrics(registry ToolRegistry, step Step) []Metric {
	var metrics []Metric
	if registry.configured() {
		metrics = append(metrics, toolRegistryMetric{registry: registry})
	}
	if len(step.RequiredTools) > 0 {
		metrics = append(metrics, RequiredTools{Names: step.RequiredTools})
	}
	if len(step.ForbiddenTools) > 0 {
		metrics = append(metrics, ForbiddenTool{Names: step.ForbiddenTools})
	}
	if step.MaxToolCalls > 0 {
		metrics = append(metrics, StepBudget{MaxSteps: step.MaxToolCalls})
	}
	return metrics
}

func (r *Runner) scoreScenarioMetric(tb testing.TB, metric Metric, c Case) (Result, error) {
	tb.Helper()

	ctx, cancel := runnerContext(r.timeout)
	defer cancel()

	start := time.Now()
	result, err := metric.Score(ctx, maybeTrace(r.judge, tb), c)
	result.Metadata = mergeMetadata(c.Metadata, result.Metadata)
	if result.Metric == "" {
		result.Metric = metric.Name()
	}
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}
	return result, err
}

func applyExpectFail(step Step, rawResults []Result, stepMetadata map[string]any) ([]Result, bool) {
	if !step.ExpectFail {
		return rawResults, allResultsPassed(rawResults)
	}

	rawPassed := allResultsPassed(rawResults)
	results := make([]Result, 0, len(rawResults)+1)
	for _, result := range rawResults {
		adjusted := result
		failedAsExpected := !result.Passed
		adjusted.Metadata = mergeMetadata(result.Metadata, map[string]any{
			"expect_fail":          true,
			"expect_fail_observed": failedAsExpected,
		})
		if !rawPassed && !result.Passed {
			adjusted.Score = 1
			adjusted.Passed = true
			adjusted.Reason = "expected failure observed: " + adjusted.Reason
		}
		results = append(results, adjusted)
	}
	if rawPassed {
		results = append(results, Result{
			Score:  0,
			Passed: false,
			Metric: "ExpectFail",
			Reason: "step expected to fail but all contracts and checks passed",
			Metadata: mergeMetadata(stepMetadata, map[string]any{
				"expect_fail":          true,
				"expect_fail_observed": false,
			}),
		})
		return results, false
	}
	return results, true
}

func allResultsPassed(results []Result) bool {
	for _, result := range results {
		if !result.Passed {
			return false
		}
	}
	return true
}

func (r *Runner) assertScenarioResult(tb testing.TB, result Result) {
	tb.Helper()
	if !result.Passed {
		tb.Errorf("%s=%.2f below threshold\nReason: %s", result.Metric, result.Score, result.Reason)
		return
	}
	tb.Logf("%s=%.2f pass (reason: %s)", result.Metric, result.Score, result.Reason)
}

func scenarioBaseMetadata(s Scenario) map[string]any {
	metadata := cloneMetadata(s.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["scenario"] = s.Name
	if s.Tier != "" {
		metadata["tier"] = s.Tier
	}
	return metadata
}

func scenarioStepMetadata(s Scenario, step Step, resultMetadata map[string]any) map[string]any {
	metadata := scenarioBaseMetadata(s)
	metadata = mergeMetadata(metadata, step.Metadata)
	metadata = mergeMetadata(metadata, resultMetadata)
	metadata["scenario"] = s.Name
	metadata["step"] = step.Name
	if s.Tier != "" {
		metadata["tier"] = s.Tier
	}
	if step.ExpectFail {
		metadata["expect_fail"] = true
	}
	return metadata
}

func mergeMetadata(base map[string]any, overlays ...map[string]any) map[string]any {
	var out map[string]any
	if base != nil {
		out = cloneMetadata(base)
	}
	for _, overlay := range overlays {
		if overlay == nil {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		for key, value := range overlay {
			out[key] = cloneAny(value)
		}
	}
	return out
}

func mergeArtifacts(dst map[string]json.RawMessage, src map[string]json.RawMessage) {
	for key, value := range src {
		dst[key] = cloneRawMessage(value)
	}
}

func cloneStep(step Step) Step {
	return Step{
		Name:           step.Name,
		Input:          step.Input,
		RequiredTools:  append([]string(nil), step.RequiredTools...),
		ForbiddenTools: append([]string(nil), step.ForbiddenTools...),
		MaxToolCalls:   step.MaxToolCalls,
		ExpectFail:     step.ExpectFail,
		Checks:         append([]Metric(nil), step.Checks...),
		Metadata:       cloneMetadata(step.Metadata),
	}
}

func scenarioStepTestName(tbName string, scenarioName string, stepName string) string {
	return tbName + "/" + scenarioName + "/" + stepName
}

type toolRegistryMetric struct {
	registry ToolRegistry
}

func (m toolRegistryMetric) Name() string { return "ToolRegistry" }

func (m toolRegistryMetric) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx
	for _, call := range flattenToolCalls(c.Turns) {
		if !m.registry.has(call.Name) {
			return Result{
				Score:  0,
				Passed: false,
				Metric: m.Name(),
				Reason: fmt.Sprintf("unknown tool used: %s", call.Name),
			}, nil
		}
	}
	return Result{
		Score:  1,
		Passed: true,
		Metric: m.Name(),
		Reason: "all observed tools are registered",
	}, nil
}
