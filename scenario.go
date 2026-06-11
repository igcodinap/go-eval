package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// ScenarioRepeat configures repeated full-scenario execution.
type ScenarioRepeat struct {
	N        int     `json:"n,omitempty"`
	PassRate float64 `json:"pass_rate,omitempty"`
}

// Scenario describes a sequential multi-step agent evaluation.
//
// State carries driver runtime state between steps. The runner clones JSON-like
// map and slice values, but custom reference values are treated as opaque and
// copied by reference; keep those values immutable or app-owned.
type Scenario struct {
	Name       string
	Tier       string
	DriverName string
	Metadata   map[string]any
	State      map[string]any
	Tools      ToolRegistry
	Driver     StepFunc
	Steps      []Step
	Repeat     ScenarioRepeat
}

// Step describes one user interaction and its expected agent behavior.
type Step struct {
	Name                  string
	Input                 string
	RequiredTools         []string
	RequiredToolPatterns  []string
	ForbiddenTools        []string
	ForbiddenToolPatterns []string
	ForbiddenToolExcept   []string
	RequiredArtifacts     []string
	ForbiddenArtifacts    []string
	MaxToolCalls          int
	ExpectFail            bool
	Checks                []Metric
	Metadata              map[string]any
	Timeout               time.Duration
}

// StepRequest is passed to a Scenario driver for each step.
type StepRequest struct {
	Step      Step
	History   []Turn
	Artifacts map[string]json.RawMessage
	State     map[string]any
}

// StepFunc runs one scenario step and returns the newly observed outputs.
type StepFunc func(ctx context.Context, req StepRequest) (StepResult, error)

// StepResult is the observed result of running one scenario step.
type StepResult struct {
	Output    string
	Turns     []Turn
	Trace     *Trace
	Artifacts map[string]json.RawMessage
	Metadata  map[string]any
	State     map[string]any
}

// ScenarioResult is the aggregate result returned by RunScenario.
type ScenarioResult struct {
	Passed     bool
	Results    []Result
	Turns      []Turn
	TraceID    string
	TraceIDs   []string
	Artifacts  map[string]json.RawMessage
	RunCount   int
	PassRuns   int
	Dimensions []DimensionResult
}

type scenarioRunResult struct {
	result ScenarioResult
	steps  []StepSummary
}

// RunScenario executes a sequential agent scenario and asserts via tb.
func (r *Runner) RunScenario(tb testing.TB, s Scenario) ScenarioResult {
	tb.Helper()

	if os.Getenv(EnvVar) == "" {
		tb.Skip("eval skipped, set " + EnvVar + "=1 to run")
		return ScenarioResult{}
	}

	filterCase := Case{Metadata: scenarioBaseMetadata(s)}
	if !r.shouldRun(filterCase) {
		tb.Skip("eval skipped by case filter")
		return ScenarioResult{}
	}

	if err := validateScenario(s); err != nil {
		tb.Fatalf("scenario %q: %v", s.Name, err)
		return ScenarioResult{}
	}

	repeat := normalizeScenarioRepeat(s.Repeat)
	if repeat.N <= 1 {
		run, ok := r.runScenarioOnce(tb, s, 0, 0, true)
		if !ok {
			return run.result
		}
		run.result.RunCount = 1
		if run.result.Passed {
			run.result.PassRuns = 1
		}
		run.result.Dimensions = scenarioRepeatDimensions(run.result.PassRuns, run.result.RunCount, repeat.PassRate)
		r.writeScenarioSummary(tb, s, run.result, run.steps)
		return run.result
	}

	out := ScenarioResult{RunCount: repeat.N}
	var summaries []StepSummary
	for runIndex := 1; runIndex <= repeat.N; runIndex++ {
		run, ok := r.runScenarioOnce(tb, s, runIndex, repeat.N, false)
		if !ok {
			return out
		}
		out.Results = append(out.Results, run.result.Results...)
		out.Turns = cloneTurns(run.result.Turns)
		out.TraceIDs = append(out.TraceIDs, run.result.TraceID)
		out.Artifacts = cloneArtifacts(run.result.Artifacts)
		summaries = append(summaries, run.steps...)
		if run.result.Passed {
			out.PassRuns++
		}
	}
	passRate := float64(out.PassRuns) / float64(out.RunCount)
	out.Passed = passRate >= repeat.PassRate
	out.Dimensions = scenarioRepeatDimensions(out.PassRuns, out.RunCount, repeat.PassRate)
	if out.Passed {
		tb.Logf("ScenarioRepeat=%.2f pass (%d/%d runs passed)", passRate, out.PassRuns, out.RunCount)
	} else {
		tb.Errorf("ScenarioRepeat=%.2f below threshold %.2f (%d/%d runs passed)", passRate, repeat.PassRate, out.PassRuns, out.RunCount)
	}
	r.writeScenarioSummary(tb, s, out, summaries)
	return out
}

func (r *Runner) runScenarioOnce(
	tb testing.TB,
	s Scenario,
	repeatRun int,
	repeatTotal int,
	assertResults bool,
) (scenarioRunResult, bool) {
	tb.Helper()

	out := ScenarioResult{Passed: true}
	var history []Turn
	artifacts := map[string]json.RawMessage{}
	state := cloneMetadata(s.State)
	var summaries []StepSummary
	trace := newScenarioTrace(tb.Name(), s, repeatRun, repeatTotal)

	for _, step := range s.Steps {
		stepResult, ok := r.runScenarioStep(tb, s, step, history, artifacts, state)
		if !ok {
			return scenarioRunResult{result: out, steps: summaries}, false
		}

		stepTurns := cloneTurns(stepResult.Turns)
		stepMetadata := scenarioStepMetadata(s, step, stepResult.Metadata, repeatRun, repeatTotal)
		spanStart := len(trace.Spans)
		artifactStart := len(trace.Artifacts)
		stateDeltaStart := len(trace.StateDeltas)
		appendScenarioStepTrace(&trace, step, stepResult, repeatRun)
		stepTrace := trace
		stepTrace.Name = step.Name
		stepTrace.TestName = scenarioStepTestName(tb.Name(), s.Name, step.Name)
		stepTrace.Spans = cloneSpans(trace.Spans[spanStart:])
		stepTrace.Artifacts = cloneArtifactRecords(trace.Artifacts[artifactStart:])
		stepTrace.StateDeltas = cloneStateDeltas(trace.StateDeltas[stateDeltaStart:])
		if stepResult.Trace != nil {
			stepTrace.Metadata = mergeMetadata(stepMetadata, stepResult.Trace.Metadata)
		} else {
			stepTrace.Metadata = stepMetadata
		}
		history = append(history, stepTurns...)
		mergeArtifacts(artifacts, stepResult.Artifacts)
		state = mergeMetadata(state, stepResult.State)

		stepCase := Case{
			Input:     step.Input,
			Output:    stepResult.Output,
			Turns:     cloneTurns(stepTurns),
			TraceID:   trace.ID,
			Trace:     &stepTrace,
			Metadata:  stepMetadata,
			Artifacts: cloneArtifacts(artifacts),
			Timeout:   step.Timeout,
		}

		rawResults, ok := r.scoreScenarioStep(tb, s, step, stepCase)
		if !ok {
			return scenarioRunResult{result: out, steps: summaries}, false
		}

		results, stepPassed := applyExpectFail(step, rawResults, stepMetadata)
		out.Passed = out.Passed && stepPassed
		out.Results = append(out.Results, results...)
		summaries = append(summaries, buildStepSummary(step, stepTurns, stepResult.Artifacts, results, stepPassed, stepMetadata, repeatRun))
		testName := scenarioStepTestName(tb.Name(), s.Name, step.Name)
		for _, result := range results {
			if assertResults {
				r.assertScenarioResult(tb, result)
			}
			r.writeResultNamed(tb, testName, result)
		}
	}

	out.Turns = cloneTurns(history)
	out.TraceID = trace.ID
	out.TraceIDs = []string{trace.ID}
	out.Artifacts = cloneArtifacts(artifacts)
	r.writeTrace(tb, trace)
	return scenarioRunResult{result: out, steps: summaries}, true
}

func validateScenario(s Scenario) error {
	if s.Name == "" {
		return errors.New("name is required")
	}
	if s.Driver == nil {
		return errors.New("driver is required")
	}
	return validateScenarioDefinition(s)
}

func validateScenarioDefinition(s Scenario) error {
	if s.Name == "" {
		return errors.New("name is required")
	}
	if err := s.Tools.Validate(); err != nil {
		return fmt.Errorf("tool registry: %w", err)
	}
	if s.Repeat.PassRate < 0 || s.Repeat.PassRate > 1 {
		return fmt.Errorf("repeat pass rate must be between 0 and 1, got %g", s.Repeat.PassRate)
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
		if err := validateToolPatterns(step.RequiredToolPatterns); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		if err := validateToolPatterns(step.ForbiddenToolPatterns); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		if err := validateArtifactKeys("required artifact", step.RequiredArtifacts); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		if err := validateArtifactKeys("forbidden artifact", step.ForbiddenArtifacts); err != nil {
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

func validateArtifactKeys(label string, keys []string) error {
	for i, key := range keys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s %d is empty", label, i+1)
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
	state map[string]any,
) (StepResult, bool) {
	tb.Helper()

	ctx, cancel := runnerContext(r.timeoutForStep(step))
	defer cancel()

	req := StepRequest{
		Step:      cloneStep(step),
		History:   cloneTurns(history),
		Artifacts: cloneArtifacts(artifacts),
		State:     cloneMetadata(state),
	}
	stepResult, err := s.Driver(ctx, req)
	if err != nil {
		tb.Fatalf("scenario %q step %q: driver error: %v", s.Name, step.Name, err)
		return StepResult{}, false
	}
	return StepResult{
		Output:    stepResult.Output,
		Turns:     cloneTurns(stepResult.Turns),
		Trace:     cloneTracePtr(stepResult.Trace),
		Artifacts: cloneArtifacts(stepResult.Artifacts),
		Metadata:  cloneMetadata(stepResult.Metadata),
		State:     cloneMetadata(stepResult.State),
	}, true
}

func (r *Runner) scoreScenarioStep(tb testing.TB, s Scenario, step Step, c Case) ([]Result, bool) {
	tb.Helper()

	metrics := scenarioStepMetrics(s.Tools, step)
	metrics = append(metrics, step.Checks...)

	results := make([]Result, 0, len(metrics))
	for _, metric := range metrics {
		result, err := r.scoreScenarioMetric(tb, metric, step, c)
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
		metrics = append(metrics, RequiredTools{Names: step.RequiredTools, Patterns: step.RequiredToolPatterns})
	} else if len(step.RequiredToolPatterns) > 0 {
		metrics = append(metrics, RequiredTools{Patterns: step.RequiredToolPatterns})
	}
	if len(step.ForbiddenTools) > 0 {
		metrics = append(metrics, ForbiddenTool{
			Names:    step.ForbiddenTools,
			Patterns: step.ForbiddenToolPatterns,
			Except:   step.ForbiddenToolExcept,
		})
	} else if len(step.ForbiddenToolPatterns) > 0 {
		metrics = append(metrics, ForbiddenTool{Patterns: step.ForbiddenToolPatterns, Except: step.ForbiddenToolExcept})
	}
	if step.MaxToolCalls > 0 {
		metrics = append(metrics, StepBudget{MaxSteps: step.MaxToolCalls})
	}
	for _, key := range step.RequiredArtifacts {
		metrics = append(metrics, ArtifactExists{Key: key})
	}
	for _, key := range step.ForbiddenArtifacts {
		metrics = append(metrics, ArtifactNotExists{Key: key})
	}
	return metrics
}

func (r *Runner) scoreScenarioMetric(tb testing.TB, metric Metric, step Step, c Case) (Result, error) {
	tb.Helper()

	ctx, cancel := runnerContext(r.timeoutForStep(step))
	defer cancel()

	start := time.Now()
	result, err := metric.Score(ctx, maybeTrace(r.judge, tb), c)
	result.Metadata = mergeMetadata(c.Metadata, result.Metadata)
	if result.Metric == "" {
		result.Metric = metric.Name()
	}
	if result.TraceID == "" {
		result.TraceID = c.TraceID
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

func scenarioStepMetadata(s Scenario, step Step, resultMetadata map[string]any, repeatRun int, repeatTotal int) map[string]any {
	metadata := scenarioBaseMetadata(s)
	metadata = mergeMetadata(metadata, step.Metadata)
	metadata = mergeMetadata(metadata, resultMetadata)
	metadata["scenario"] = s.Name
	metadata["step"] = step.Name
	if repeatRun > 0 {
		metadata["repeat_run"] = repeatRun
		metadata["repeat_total"] = repeatTotal
	}
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
		Name:                  step.Name,
		Input:                 step.Input,
		RequiredTools:         append([]string(nil), step.RequiredTools...),
		RequiredToolPatterns:  append([]string(nil), step.RequiredToolPatterns...),
		ForbiddenTools:        append([]string(nil), step.ForbiddenTools...),
		ForbiddenToolPatterns: append([]string(nil), step.ForbiddenToolPatterns...),
		ForbiddenToolExcept:   append([]string(nil), step.ForbiddenToolExcept...),
		RequiredArtifacts:     append([]string(nil), step.RequiredArtifacts...),
		ForbiddenArtifacts:    append([]string(nil), step.ForbiddenArtifacts...),
		MaxToolCalls:          step.MaxToolCalls,
		ExpectFail:            step.ExpectFail,
		Checks:                append([]Metric(nil), step.Checks...),
		Metadata:              cloneMetadata(step.Metadata),
		Timeout:               step.Timeout,
	}
}

func scenarioStepTestName(tbName string, scenarioName string, stepName string) string {
	return tbName + "/" + scenarioName + "/" + stepName
}

func normalizeScenarioRepeat(repeat ScenarioRepeat) ScenarioRepeat {
	if repeat.N <= 1 {
		repeat.N = 1
	}
	if repeat.PassRate == 0 {
		repeat.PassRate = 1
	}
	return repeat
}

func scenarioRepeatDimensions(passRuns int, runCount int, requiredPassRate float64) []DimensionResult {
	if runCount <= 0 {
		return nil
	}
	passRate := float64(passRuns) / float64(runCount)
	passed := passRate >= requiredPassRate
	return []DimensionResult{
		{Name: "pass_rate", Score: passRate, Threshold: requiredPassRate, Passed: passed},
		{Name: "pass_runs", Score: float64(passRuns), Threshold: float64(runCount) * requiredPassRate, Passed: passed},
	}
}

func (r *Runner) timeoutForStep(step Step) time.Duration {
	if step.Timeout > 0 {
		return step.Timeout
	}
	return r.timeout
}

func buildStepSummary(
	step Step,
	turns []Turn,
	artifacts map[string]json.RawMessage,
	results []Result,
	passed bool,
	metadata map[string]any,
	repeatRun int,
) StepSummary {
	summary := StepSummary{
		Name:         step.Name,
		Passed:       passed,
		RepeatRun:    repeatRun,
		ToolCalls:    toolCallNames(turns),
		ArtifactKeys: sortedArtifactKeys(artifacts),
		Metadata:     cloneMetadata(metadata),
	}
	for _, result := range results {
		if !result.Passed {
			summary.FailedMetrics = append(summary.FailedMetrics, result.Metric)
		}
	}
	sort.Strings(summary.FailedMetrics)
	return summary
}

func toolCallNames(turns []Turn) []string {
	var names []string
	for _, call := range flattenToolCalls(turns) {
		names = append(names, call.Name)
	}
	sort.Strings(names)
	return names
}

func sortedArtifactKeys(artifacts map[string]json.RawMessage) []string {
	if len(artifacts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(artifacts))
	for key := range artifacts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *Runner) writeScenarioSummary(tb testing.TB, s Scenario, result ScenarioResult, steps []StepSummary) {
	tb.Helper()
	if r.sink == nil {
		return
	}
	score := 0.0
	if result.RunCount > 0 {
		score = float64(result.PassRuns) / float64(result.RunCount)
	}
	reason := fmt.Sprintf("%d/%d scenario runs passed", result.PassRuns, result.RunCount)
	metadata := scenarioBaseMetadata(s)
	runResult := newRunResult(scenarioSummaryTestName(tb.Name(), s.Name), Result{
		Score:      score,
		Passed:     result.Passed,
		Metric:     "_scenario_summary",
		TraceID:    result.TraceID,
		Reason:     reason,
		Dimensions: result.Dimensions,
		Metadata:   metadata,
	})
	runResult.Kind = runResultKindScenarioSummary
	runResult.ScenarioName = s.Name
	runResult.ScenarioSummary = &ScenarioSummary{
		Name:         s.Name,
		Passed:       result.Passed,
		RunCount:     result.RunCount,
		PassRuns:     result.PassRuns,
		TraceIDs:     append([]string(nil), result.TraceIDs...),
		Steps:        steps,
		Metadata:     metadata,
		ArtifactKeys: sortedArtifactKeys(result.Artifacts),
		Dimensions:   result.Dimensions,
	}
	r.writeRunResult(tb, runResult)
}

func scenarioSummaryTestName(tbName string, scenarioName string) string {
	return tbName + "/" + scenarioName
}

func newScenarioTrace(tbName string, s Scenario, repeatRun int, repeatTotal int) Trace {
	metadata := scenarioBaseMetadata(s)
	if repeatRun > 0 {
		metadata["repeat_run"] = repeatRun
		metadata["repeat_total"] = repeatTotal
	}
	trace := Trace{
		ID:           newTraceID(),
		Name:         s.Name,
		TestName:     scenarioSummaryTestName(tbName, s.Name),
		ScenarioName: s.Name,
		StartedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:     metadata,
	}
	return trace
}

func appendScenarioStepTrace(trace *Trace, step Step, result StepResult, repeatRun int) {
	if trace == nil {
		return
	}
	span := Span{
		ID:       trace.ID + "/" + step.Name,
		Name:     step.Name,
		Kind:     "scenario_step",
		Input:    step.Input,
		Output:   result.Output,
		Metadata: mergeMetadata(step.Metadata, result.Metadata),
	}
	if repeatRun > 0 {
		span.Metadata = mergeMetadata(span.Metadata, map[string]any{"repeat_run": repeatRun})
	}
	trace.Spans = append(trace.Spans, span)
	if !traceHasToolCalls(result.Trace) {
		for _, call := range flattenToolCalls(result.Turns) {
			toolCall := call
			trace.Spans = append(trace.Spans, Span{
				ID:       trace.ID + "/" + step.Name + "/tool/" + call.Name,
				ParentID: span.ID,
				Name:     call.Name,
				Kind:     "tool_call",
				ToolCall: &toolCall,
				Error:    call.Error,
				Metadata: cloneMetadata(call.Metadata),
			})
		}
	}
	for key, value := range result.Artifacts {
		trace.Artifacts = append(trace.Artifacts, ArtifactRecord{
			Key:   key,
			Name:  key,
			Value: cloneRawMessage(value),
			Metadata: map[string]any{
				"step": step.Name,
			},
		})
	}
	if result.Trace != nil {
		trace.Spans = append(trace.Spans, cloneSpans(result.Trace.Spans)...)
		trace.Artifacts = append(trace.Artifacts, cloneArtifactRecords(result.Trace.Artifacts)...)
		trace.StateDeltas = append(trace.StateDeltas, cloneStateDeltas(result.Trace.StateDeltas)...)
		trace.Metadata = mergeMetadata(trace.Metadata, result.Trace.Metadata)
	}
	trace.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

func traceHasToolCalls(trace *Trace) bool {
	if trace == nil {
		return false
	}
	for _, span := range trace.Spans {
		if span.ToolCall != nil {
			return true
		}
	}
	return false
}

type toolRegistryMetric struct {
	registry ToolRegistry
}

func (m toolRegistryMetric) Name() string { return "ToolRegistry" }

func (m toolRegistryMetric) Score(ctx context.Context, _ Judge, c Case) (Result, error) {
	_ = ctx
	for _, call := range toolCallsFromCase(c) {
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
