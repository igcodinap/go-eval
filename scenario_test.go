package eval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunScenario_SkipsWhenGoevalUnset(t *testing.T) {
	t.Setenv(EnvVar, "")

	var calls int
	sink := &recordingSink{}
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	got := r.RunScenario(tb, Scenario{
		Name: "skipped",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			calls++
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one"}},
	})

	if !tb.skipped || calls != 0 || sink.count() != 0 {
		t.Fatalf("expected skip without work, skipped=%v calls=%d sink=%d", tb.skipped, calls, sink.count())
	}
	if got.Passed || len(got.Results) != 0 {
		t.Fatalf("expected zero scenario result, got %+v", got)
	}
}

func TestRunScenario_WithCaseFilterSkipsScenario(t *testing.T) {
	t.Setenv(EnvVar, "1")

	var calls int
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithCaseFilter(func(c Case) bool {
		return c.Metadata["tier"] == "critical"
	}))
	got := r.RunScenario(tb, Scenario{
		Name: "filtered",
		Tier: "standard",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			calls++
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one"}},
	})

	if !tb.skipped || calls != 0 || len(got.Results) != 0 {
		t.Fatalf("expected scenario filter skip, skipped=%v calls=%d result=%+v", tb.skipped, calls, got)
	}
}

func TestRunScenario_AccumulatesHistoryArtifactsAndWritesMetadata(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	var step2SawHistory bool
	var step2SawArtifact bool
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	got := r.RunScenario(t, Scenario{
		Name:     "planning_to_route_ready",
		Tier:     "critical",
		Metadata: map[string]any{"suite": "route"},
		Tools:    NewToolRegistry("plan_route"),
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			switch req.Step.Name {
			case "plan":
				return StepResult{
					Output: "Route is ready",
					Turns: []Turn{{
						Role: RoleAssistant,
						ToolCalls: []ToolCall{{
							Name:      "plan_route",
							Arguments: json.RawMessage(`{"from":"USACH"}`),
						}},
					}},
					Artifacts: map[string]json.RawMessage{
						"route": json.RawMessage(`{"status":"ready","stops":["USACH","Pajaritos"]}`),
					},
					Metadata: map[string]any{"trip_plan_id": "trip-123"},
				}, nil
			case "verify":
				step2SawHistory = len(req.History) == 1 &&
					len(req.History[0].ToolCalls) == 1 &&
					string(req.History[0].ToolCalls[0].Arguments) == `{"from":"USACH"}`
				step2SawArtifact = string(req.Artifacts["route"]) == `{"status":"ready","stops":["USACH","Pajaritos"]}`
				req.History[0].ToolCalls[0].Arguments[0] = 'X'
				req.Artifacts["route"][0] = 'X'
				return StepResult{
					Output: "Still ready",
					Artifacts: map[string]json.RawMessage{
						"budget": json.RawMessage(`{"tokens":12}`),
					},
				}, nil
			default:
				t.Fatalf("unexpected step %q", req.Step.Name)
				return StepResult{}, nil
			}
		},
		Steps: []Step{
			{
				Name:          "plan",
				Input:         "Propón la ruta",
				RequiredTools: []string{"plan_route"},
				MaxToolCalls:  1,
				Checks: []Metric{
					ArtifactArrayMinLen{Key: "route", Path: "stops", MinLen: 2},
				},
			},
			{
				Name:  "verify",
				Input: "Confirma",
				Checks: []Metric{
					ArtifactJSONPath{Key: "route", Path: "status", Expected: "ready"},
					ArtifactJSONPath{Key: "budget", Path: "tokens", Expected: "12"},
				},
			},
		},
	})

	if !got.Passed {
		t.Fatalf("expected scenario pass, got %+v", got)
	}
	if !step2SawHistory || !step2SawArtifact {
		t.Fatalf("driver did not receive accumulated copies, history=%v artifact=%v", step2SawHistory, step2SawArtifact)
	}
	if len(got.Turns) != 1 || string(got.Turns[0].ToolCalls[0].Arguments) != `{"from":"USACH"}` {
		t.Fatalf("history was not preserved: %+v", got.Turns)
	}
	if string(got.Artifacts["route"]) != `{"status":"ready","stops":["USACH","Pajaritos"]}` ||
		string(got.Artifacts["budget"]) != `{"tokens":12}` {
		t.Fatalf("unexpected artifacts: %+v", got.Artifacts)
	}
	if sink.count() != len(got.Results)+1 {
		t.Fatalf("expected one sink row per result plus summary, sink=%d results=%d", sink.count(), len(got.Results))
	}
	written := sink.results[len(sink.results)-2]
	if written.Metadata["scenario"] != "planning_to_route_ready" ||
		written.Metadata["step"] != "verify" ||
		written.Metadata["tier"] != "critical" ||
		written.Metadata["suite"] != "route" {
		t.Fatalf("unexpected metadata: %+v", written.Metadata)
	}
	if !strings.Contains(written.TestName, "planning_to_route_ready/verify") {
		t.Fatalf("unexpected test name: %q", written.TestName)
	}
}

func TestRunScenario_ContractsExpectFailAndContinuation(t *testing.T) {
	t.Setenv(EnvVar, "1")

	var calls int
	sink := &recordingSink{}
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	got := r.RunScenario(tb, Scenario{
		Name:  "contracts",
		Tools: NewToolRegistry("safe_tool", "danger_tool"),
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			calls++
			switch req.Step.Name {
			case "forbidden":
				return StepResult{Turns: []Turn{{ToolCalls: []ToolCall{{Name: "danger_tool"}}}}}, nil
			case "expected_fail":
				return StepResult{}, nil
			case "continues":
				return StepResult{Output: "done"}, nil
			default:
				return StepResult{}, nil
			}
		},
		Steps: []Step{
			{Name: "forbidden", ForbiddenTools: []string{"danger_tool"}},
			{Name: "expected_fail", RequiredTools: []string{"safe_tool"}, ExpectFail: true},
			{Name: "continues", Checks: []Metric{scriptedMetric{
				name:   "OutputNonEmpty",
				result: Result{Score: 1, Passed: true, Metric: "OutputNonEmpty", Reason: "output was produced"},
			}}},
		},
	})

	if got.Passed {
		t.Fatalf("expected scenario to fail because first step violated contract")
	}
	if calls != 3 {
		t.Fatalf("expected later steps to continue after contract failure, calls=%d", calls)
	}
	if !tb.errored {
		t.Fatalf("expected Errorf for non-expected failure")
	}
	var sawExpectFail bool
	for _, result := range got.Results {
		if result.Metadata["step"] == "expected_fail" &&
			result.Metadata["expect_fail"] == true &&
			result.Metadata["expect_fail_observed"] == true &&
			result.Passed &&
			result.Score == 1 {
			sawExpectFail = true
		}
	}
	if !sawExpectFail {
		t.Fatalf("expected transformed expect-fail result, results=%+v", got.Results)
	}
	if sink.count() != len(got.Results)+1 {
		t.Fatalf("expected sink rows for all results")
	}
}

func TestRunScenario_ExpectFailUnexpectedPassFails(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name: "unexpected_pass",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "empty", ExpectFail: true}},
	})

	if got.Passed || !tb.errored {
		t.Fatalf("expected unexpected pass to fail, result=%+v errored=%v", got, tb.errored)
	}
	if len(got.Results) != 1 || got.Results[0].Metric != "ExpectFail" {
		t.Fatalf("expected synthetic ExpectFail result, got %+v", got.Results)
	}
}

func TestRunScenario_FatalErrors(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tests := []struct {
		name     string
		scenario Scenario
	}{
		{
			name: "nil driver",
			scenario: Scenario{
				Name:  "nil_driver",
				Steps: []Step{{Name: "one"}},
			},
		},
		{
			name: "driver error",
			scenario: Scenario{
				Name: "driver_error",
				Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
					return StepResult{}, errors.New("boom")
				},
				Steps: []Step{{Name: "one"}},
			},
		},
		{
			name: "nil check",
			scenario: Scenario{
				Name: "nil_check",
				Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
					return StepResult{}, nil
				},
				Steps: []Step{{Name: "one", Checks: []Metric{nil}}},
			},
		},
		{
			name: "unknown configured tool",
			scenario: Scenario{
				Name:  "unknown_configured_tool",
				Tools: NewToolRegistry("known"),
				Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
					return StepResult{}, nil
				},
				Steps: []Step{{Name: "one", RequiredTools: []string{"missing"}}},
			},
		},
		{
			name: "metric error",
			scenario: Scenario{
				Name: "metric_error",
				Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
					return StepResult{}, nil
				},
				Steps: []Step{{Name: "one", Checks: []Metric{scriptedMetric{name: "X", err: errors.New("judge")}}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := &recordingTB{}
			_ = NewRunner(&MockJudge{}).RunScenario(tb, tt.scenario)
			if !tb.fataled {
				t.Fatalf("expected fatal for %s", tt.name)
			}
		})
	}
}

func TestRunScenario_UnknownObservedToolIsContractFailure(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name:  "unknown_observed",
		Tools: NewToolRegistry("known"),
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{Turns: []Turn{{ToolCalls: []ToolCall{{Name: "other"}}}}}, nil
		},
		Steps: []Step{{Name: "one"}},
	})

	if got.Passed || !tb.errored || len(got.Results) != 1 || got.Results[0].Metric != "ToolRegistry" {
		t.Fatalf("expected unknown observed tool failure, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_ToolRegistrySeesTraceOnlyStepTools(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name:  "trace_only_unknown_observed",
		Tools: NewToolRegistry("known"),
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{
				Trace: &Trace{Spans: []Span{{
					Kind:     "tool_call",
					ToolCall: &ToolCall{Name: "other"},
				}}},
			}, nil
		},
		Steps: []Step{{Name: "one"}},
	})

	if got.Passed || !tb.errored || len(got.Results) != 1 || got.Results[0].Metric != "ToolRegistry" {
		t.Fatalf("expected trace-only unknown tool failure, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_StepToolContractsUseCurrentStepTraceOnly(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name: "current_step_trace_contracts",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			if req.Step.Name == "first" {
				return StepResult{
					Trace: &Trace{Spans: []Span{{
						Kind:     "tool_call",
						ToolCall: &ToolCall{Name: "safe_tool"},
					}}},
				}, nil
			}
			return StepResult{}, nil
		},
		Steps: []Step{
			{Name: "first"},
			{Name: "second", RequiredTools: []string{"safe_tool"}, ExpectFail: true},
		},
	})

	if !got.Passed || tb.errored {
		t.Fatalf("expected second step not to inherit first step trace tools, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_TraceToolCallsAreAuthoritativeOverTurnToolCalls(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name: "trace_authoritative_tool_calls",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{
				Turns: []Turn{{
					Role:      RoleAssistant,
					ToolCalls: []ToolCall{{Name: "search"}},
				}},
				Trace: &Trace{Spans: []Span{{
					Kind:     "tool_call",
					ToolCall: &ToolCall{Name: "search"},
				}}},
			}, nil
		},
		Steps: []Step{{Name: "one", MaxToolCalls: 1}},
	})

	if !got.Passed || tb.errored {
		t.Fatalf("expected duplicate turn/trace tool observation to count once, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_CheckUsesRunnerJudgeAndTimeout(t *testing.T) {
	t.Setenv(EnvVar, "1")

	var sawJudge bool
	var sawDeadline bool
	check := funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
		sawJudge = j != nil
		_, sawDeadline = ctx.Deadline()
		return Result{Score: 1, Passed: true, Metric: "Check"}, nil
	})

	got := NewRunner(&MockJudge{}, WithTimeout(time.Second)).RunScenario(t, Scenario{
		Name: "check_context",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one", Checks: []Metric{check}}},
	})

	if !got.Passed || !sawJudge || !sawDeadline {
		t.Fatalf("expected check to receive runner judge and deadline, passed=%v judge=%v deadline=%v", got.Passed, sawJudge, sawDeadline)
	}
}

func TestRunScenario_ParallelSharedRunner(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	var calls atomic.Int64

	for _, name := range []string{"a", "b"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := r.RunScenario(t, Scenario{
				Name: "parallel_" + name,
				Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
					calls.Add(1)
					return StepResult{Output: "ok"}, nil
				},
				Steps: []Step{{Name: "one", Checks: []Metric{scriptedMetric{
					name:   "OutputOK",
					result: Result{Score: 1, Passed: true, Metric: "OutputOK", Reason: "ok"},
				}}}},
			})
			if !got.Passed {
				t.Fatalf("scenario failed: %+v", got)
			}
		})
	}
}

func TestRunScenario_TierEnvFiltersScenario(t *testing.T) {
	t.Setenv(EnvVar, "1")
	t.Setenv(TierEnvVar, "critical")

	var calls int
	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}, DefaultTierFilter()).RunScenario(tb, Scenario{
		Name: "nightly_only",
		Tier: "extended",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			calls++
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one"}},
	})

	if !tb.skipped || calls != 0 || len(got.Results) != 0 {
		t.Fatalf("expected tier env skip, skipped=%v calls=%d result=%+v", tb.skipped, calls, got)
	}
}

func TestRunScenario_StateIsolatedAndResultStateAccumulatesAcrossSteps(t *testing.T) {
	t.Setenv(EnvVar, "1")

	var secondSawInitial bool
	var secondSawFirst bool
	var secondSawMutation bool
	got := NewRunner(&MockJudge{}).RunScenario(t, Scenario{
		Name:  "stateful",
		State: map[string]any{"trip_plan_id": "initial"},
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			switch req.Step.Name {
			case "one":
				req.State["trip_plan_id"] = "mutated"
				return StepResult{State: map[string]any{"summary": "first"}}, nil
			case "two":
				secondSawInitial = req.State["trip_plan_id"] == "initial"
				secondSawFirst = req.State["summary"] == "first"
				secondSawMutation = req.State["trip_plan_id"] == "mutated"
				return StepResult{}, nil
			default:
				t.Fatalf("unexpected step %q", req.Step.Name)
				return StepResult{}, nil
			}
		},
		Steps: []Step{{Name: "one"}, {Name: "two"}},
	})

	if !got.Passed || !secondSawInitial || !secondSawFirst || secondSawMutation {
		t.Fatalf("expected isolated request mutation and accumulated result state, result=%+v initial=%v first=%v mutation=%v", got, secondSawInitial, secondSawFirst, secondSawMutation)
	}
}

func TestRunScenario_StepTimeoutOverridesRunnerTimeout(t *testing.T) {
	t.Setenv(EnvVar, "1")

	var driverDeadline time.Time
	var metricDeadline time.Time
	check := funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
		var ok bool
		metricDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatalf("expected metric deadline")
		}
		return Result{Score: 1, Passed: true, Metric: "Check"}, nil
	})

	start := time.Now()
	got := NewRunner(&MockJudge{}, WithTimeout(time.Hour)).RunScenario(t, Scenario{
		Name: "step_timeout",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			var ok bool
			driverDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatalf("expected driver deadline")
			}
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one", Timeout: 20 * time.Millisecond, Checks: []Metric{check}}},
	})

	if !got.Passed || driverDeadline.Sub(start) > time.Second || metricDeadline.Sub(start) > time.Second {
		t.Fatalf("step timeout did not override runner timeout: result=%+v driver=%s metric=%s start=%s", got, driverDeadline, metricDeadline, start)
	}
}

func TestRunScenario_RepeatAggregatesPassRateAndMetadata(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	var calls int
	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}, WithResultSink(sink)).RunScenario(tb, Scenario{
		Name:   "flaky",
		Repeat: ScenarioRepeat{N: 3, PassRate: 2.0 / 3.0},
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			calls++
			return StepResult{Output: "ok"}, nil
		},
		Steps: []Step{{Name: "one", Checks: []Metric{funcMetric(func(ctx context.Context, j Judge, c Case) (Result, error) {
			repeatRun, _ := c.Metadata["repeat_run"].(int)
			passed := repeatRun != 2
			return Result{Score: boolScore(passed), Passed: passed, Metric: "Flaky", Reason: "scripted"}, nil
		})}}},
	})

	if !got.Passed || tb.errored || calls != 3 || got.RunCount != 3 || got.PassRuns != 2 {
		t.Fatalf("unexpected repeat result: result=%+v errored=%v calls=%d", got, tb.errored, calls)
	}
	results := sink.all()
	if len(results) != 4 {
		t.Fatalf("expected 3 metric rows plus summary, got %d", len(results))
	}
	if results[0].Metadata["repeat_run"] != 1 || results[1].Metadata["repeat_run"] != 2 || results[2].Metadata["repeat_run"] != 3 {
		t.Fatalf("repeat metadata missing: %+v", results)
	}
	if results[3].Kind != runResultKindScenarioSummary || results[3].ScenarioSummary.PassRuns != 2 {
		t.Fatalf("missing scenario summary: %+v", results[3])
	}
	if results[3].TraceID != "" {
		t.Fatalf("repeated scenario summary should not point to one trace id: %+v", results[3])
	}
	if len(results[3].ScenarioSummary.TraceIDs) != 3 {
		t.Fatalf("repeated scenario summary should keep all trace ids: %+v", results[3].ScenarioSummary.TraceIDs)
	}
}

func TestRunScenario_RepeatFailsBelowPassRate(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name:   "flaky_fail",
		Repeat: ScenarioRepeat{N: 3, PassRate: 1},
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one", Checks: []Metric{scriptedMetric{
			name:   "X",
			result: Result{Score: 0, Passed: false, Metric: "X", Reason: "fail"},
		}}}},
	})

	if got.Passed || !tb.errored || got.PassRuns != 0 {
		t.Fatalf("expected aggregate repeat failure, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_StepToolPatterns(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	got := NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name: "tool_patterns",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{Turns: []Turn{{ToolCalls: []ToolCall{{Name: "route_plan"}}}}}, nil
		},
		Steps: []Step{{
			Name:                  "one",
			RequiredToolPatterns:  []string{"route_*"},
			ForbiddenToolPatterns: []string{"delete_*"},
		}},
	})

	if !got.Passed || tb.errored {
		t.Fatalf("expected step tool patterns to pass, result=%+v errored=%v", got, tb.errored)
	}
}

func TestRunScenario_InvalidStepToolPatternFatal(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	_ = NewRunner(&MockJudge{}).RunScenario(tb, Scenario{
		Name: "bad_pattern",
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{}, nil
		},
		Steps: []Step{{Name: "one", RequiredToolPatterns: []string{"route_["}}},
	})

	if !tb.fataled {
		t.Fatalf("expected invalid pattern fatal")
	}
}

func boolScore(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
