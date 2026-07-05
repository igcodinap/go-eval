package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingSink struct {
	mu      sync.Mutex
	results []RunResult
	err     error
}

func (s *recordingSink) Write(result RunResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, result)
	return s.err
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.results)
}

func (s *recordingSink) last() RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.results) == 0 {
		panic("recordingSink.last called with no results")
	}
	return s.results[len(s.results)-1]
}

func (s *recordingSink) all() []RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RunResult(nil), s.results...)
}

type concurrencyObservingSink struct {
	entered  chan struct{}
	release  chan struct{}
	writes   atomic.Int64
	inFlight atomic.Int64
	max      atomic.Int64
}

func (s *concurrencyObservingSink) Write(RunResult) error {
	current := s.inFlight.Add(1)
	updateMaxInt64(&s.max, current)
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		<-s.release
	}
	s.inFlight.Add(-1)
	s.writes.Add(1)
	return nil
}

func updateMaxInt64(target *atomic.Int64, value int64) {
	for {
		old := target.Load()
		if value <= old || target.CompareAndSwap(old, value) {
			return
		}
	}
}

func TestRunner_WritesToSinkWhenConfigured(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	_ = r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 0.9, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{})

	if sink.count() != 1 {
		t.Fatalf("expected one sink write, got %d", sink.count())
	}
}

func TestRunner_AllowsConcurrentSinkWrites(t *testing.T) {
	const writes = 4

	sink := &concurrencyObservingSink{
		entered: make(chan struct{}, writes),
		release: make(chan struct{}),
	}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	tb := &recordingTB{}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r.writeRunResult(tb, RunResult{TestName: "test", Metric: "X", Passed: true})
		}()
	}

	close(start)
	entered := 0
	timeout := time.After(200 * time.Millisecond)
	for entered < writes {
		select {
		case <-sink.entered:
			entered++
		case <-timeout:
			close(sink.release)
			wg.Wait()
			t.Fatalf("only %d/%d sink writes entered concurrently", entered, writes)
		}
	}
	close(sink.release)
	wg.Wait()

	if got := sink.writes.Load(); got != writes {
		t.Fatalf("sink writes = %d, want %d", got, writes)
	}
	if got := sink.max.Load(); got < 2 {
		t.Fatalf("max concurrent sink writes = %d, want at least 2", got)
	}
}

func TestRunner_SinkErrorUsesErrorf(t *testing.T) {
	t.Setenv(EnvVar, "1")

	tb := &recordingTB{}
	sink := &recordingSink{err: errors.New("disk full")}

	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	_ = r.Run(tb, scriptedMetric{
		name:   "X",
		result: Result{Score: 0.9, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{})

	if !tb.errored {
		t.Fatalf("expected Errorf when sink write fails")
	}
}

func TestDefaultResultSink_UnsetReturnsNil(t *testing.T) {
	t.Setenv(ResultsDirEnvVar, "")
	if sink := DefaultResultSink(); sink != nil {
		t.Fatalf("expected nil sink, got %#v", sink)
	}
}

func TestDefaultResultSink_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ResultsDirEnvVar, dir)

	sink := DefaultResultSink()
	if sink == nil {
		t.Fatalf("expected non-nil sink")
	}
	if err := sink.Write(RunResult{
		TestName:         "t",
		Metric:           "m",
		Score:            1,
		Tokens:           11,
		PromptTokens:     4,
		CompletionTokens: 7,
		Metadata:         map[string]any{"suite": "conversation", "user_language": "spanish"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	p := filepath.Join(dir, "results.jsonl")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected sink output file %s: %v", p, err)
	}

	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	var got RunResult
	if err := json.NewDecoder(f).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Metadata["suite"] != "conversation" || got.Metadata["user_language"] != "spanish" {
		t.Fatalf("unexpected metadata: %+v", got.Metadata)
	}
	if got.Tokens != 11 || got.PromptTokens != 4 || got.CompletionTokens != 7 {
		t.Fatalf("unexpected token fields: %+v", got)
	}
}

func TestJSONLResultSinkCurrentRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	sink := NewJSONLResultSink(path)
	if sink == nil {
		t.Fatalf("expected non-nil sink")
	}

	result := RunResult{
		Timestamp:        "2026-07-05T12:00:00Z",
		TestName:         "TestRAG/france",
		Metric:           "Rubric",
		ScenarioName:     "rag_answer",
		TraceID:          "trace-1",
		Score:            0.92,
		Passed:           true,
		Reason:           "grounded answer",
		Tokens:           42,
		PromptTokens:     30,
		CompletionTokens: 12,
		LatencyNS:        int64(150 * time.Millisecond),
		Dimensions: []DimensionResult{
			{Name: "faithfulness", Score: 0.9, Threshold: 0.8, Passed: true, Reason: "supported"},
		},
		Metadata: map[string]any{
			"case_id": "rag/france",
			"tier":    "critical",
		},
	}
	summary := RunResult{
		Timestamp:    "2026-07-05T12:00:01Z",
		Kind:         runResultKindScenarioSummary,
		TestName:     "TestScenario/summary",
		Metric:       "Scenario",
		ScenarioName: "rag_answer",
		Score:        1,
		Passed:       true,
		ScenarioSummary: &ScenarioSummary{
			Name:     "rag_answer",
			Passed:   true,
			RunCount: 1,
			PassRuns: 1,
			TraceIDs: []string{"trace-1"},
			Steps: []StepSummary{{
				Name:         "answer",
				Passed:       true,
				ToolCalls:    []string{"retrieve"},
				ArtifactKeys: []string{"retrieval"},
				Metadata:     map[string]any{"tier": "critical"},
			}},
			Metadata:     map[string]any{"suite": "rag"},
			ArtifactKeys: []string{"retrieval"},
			Dimensions:   []DimensionResult{{Name: "scenario_pass_rate", Score: 1, Threshold: 1, Passed: true}},
		},
	}
	if err := sink.Write(result); err != nil {
		t.Fatalf("Write result: %v", err)
	}
	if err := sink.Write(summary); err != nil {
		t.Fatalf("Write summary: %v", err)
	}

	rows := readRunResultsJSONL(t, path)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].TraceID != "trace-1" ||
		rows[0].PromptTokens != 30 ||
		rows[0].CompletionTokens != 12 ||
		len(rows[0].Dimensions) != 1 ||
		rows[0].Metadata["tier"] != "critical" {
		t.Fatalf("unexpected result row: %+v", rows[0])
	}
	if rows[1].Kind != runResultKindScenarioSummary ||
		rows[1].ScenarioSummary == nil ||
		rows[1].ScenarioSummary.TraceIDs[0] != "trace-1" ||
		rows[1].ScenarioSummary.Steps[0].ToolCalls[0] != "retrieve" {
		t.Fatalf("unexpected scenario summary row: %+v", rows[1])
	}
}

func TestJSONLFileSink_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "results.jsonl")
	sink := &jsonlFileSink{path: p}

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = sink.Write(RunResult{
				TestName: "test",
				Metric:   "metric",
				Score:    float64(i),
			})
		}(i)
	}
	wg.Wait()

	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if lines != n {
		t.Fatalf("expected %d lines, got %d", n, lines)
	}
}

func TestNewRunResult(t *testing.T) {
	rr := newRunResult("test/name", Result{
		Score:            0.5,
		Passed:           true,
		Metric:           "MetricX",
		Reason:           "ok",
		Tokens:           9,
		PromptTokens:     3,
		CompletionTokens: 6,
		Metadata:         map[string]any{"trace_id": "abc"},
	})
	if rr.TestName != "test/name" || rr.Metric != "MetricX" {
		t.Fatalf("unexpected run result: %+v", rr)
	}
	if rr.Timestamp == "" {
		t.Fatalf("expected timestamp to be populated")
	}
	if rr.Metadata["trace_id"] != "abc" {
		t.Fatalf("unexpected metadata: %+v", rr.Metadata)
	}
	if rr.Tokens != 9 || rr.PromptTokens != 3 || rr.CompletionTokens != 6 {
		t.Fatalf("unexpected token fields: %+v", rr)
	}
}

func TestWithResultSinkOption(t *testing.T) {
	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	if r.sink == nil {
		t.Fatalf("expected sink option to set runner sink")
	}
}

func TestResultSinkWriteDuringMetricFailureDoesNotRun(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	tb := &recordingTB{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	_ = r.Run(tb, scriptedMetric{name: "X", err: errors.New("boom")}, Case{})

	if sink.count() != 0 {
		t.Fatalf("expected no sink writes on metric error")
	}
}

func TestRunner_CarriesCaseMetadataToSink(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	got := r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 0.9, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{Metadata: map[string]any{
		"scenario":      "empty_thread",
		"suite":         "conversation",
		"user_language": "spanish",
	}})

	if got.Metadata["suite"] != "conversation" {
		t.Fatalf("expected returned result metadata, got %+v", got.Metadata)
	}
	written := sink.last()
	if written.Metadata["scenario"] != "empty_thread" ||
		written.Metadata["suite"] != "conversation" ||
		written.Metadata["user_language"] != "spanish" {
		t.Fatalf("unexpected sink metadata: %+v", written.Metadata)
	}
}

func TestRunner_CopiesCaseMetadata(t *testing.T) {
	t.Setenv(EnvVar, "1")

	caseMetadata := map[string]any{"suite": "case"}
	r := NewRunner(&MockJudge{})
	got := r.Run(t, scriptedMetric{
		name:   "X",
		result: Result{Score: 0.9, Passed: true, Metric: "X", Reason: "ok"},
	}, Case{Metadata: caseMetadata})

	got.Metadata["suite"] = "mutated"
	if caseMetadata["suite"] != "case" {
		t.Fatalf("expected case metadata not to be mutated, got %+v", caseMetadata)
	}
}

func TestRunner_PreservesMetricMetadata(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(&MockJudge{}, WithResultSink(sink))
	metricMetadata := map[string]any{"suite": "metric"}
	got := r.Run(t, scriptedMetric{
		name: "X",
		result: Result{
			Score:    0.9,
			Passed:   true,
			Metric:   "X",
			Reason:   "ok",
			Metadata: metricMetadata,
		},
	}, Case{Metadata: map[string]any{"suite": "case"}})

	if got.Metadata["suite"] != "metric" {
		t.Fatalf("expected metric metadata to win, got %+v", got.Metadata)
	}
	written := sink.last()
	if written.Metadata["suite"] != "metric" {
		t.Fatalf("expected metric metadata in sink, got %+v", written.Metadata)
	}
}

func TestRunResultIncludesDimensions(t *testing.T) {
	res := Result{
		Score:  0.8,
		Passed: true,
		Metric: "Compound",
		Dimensions: []DimensionResult{
			{Name: "lang", Score: 0.8},
		},
	}
	rr := newRunResult("t", res)
	if len(rr.Dimensions) != 1 || rr.Dimensions[0].Name != "lang" {
		t.Fatalf("unexpected dimensions in run result: %+v", rr.Dimensions)
	}
}

func TestResultSinkCanBeUsedOutsideRunner(t *testing.T) {
	s := &recordingSink{}
	err := s.Write(newRunResult("t", Result{Score: 1, Passed: true, Metric: "X"}))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if s.count() != 1 {
		t.Fatalf("expected 1 write, got %d", s.count())
	}
}

func TestResultSinkWithContextIgnored(t *testing.T) {
	_ = context.Background()
	// This test intentionally locks API shape expectations around RunResult.
	rr := RunResult{TestName: "x", Metric: "y"}
	if rr.TestName != "x" || rr.Metric != "y" {
		t.Fatalf("unexpected RunResult values: %+v", rr)
	}
}

func TestRunner_RedactsSinkResult(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(sink),
		WithRedactors(UUIDRedactor(), FieldRedactor("trip_plan_id")),
	)
	originalMetadata := map[string]any{
		"trip_plan_id": "trip-123",
		"nested": map[string]any{
			"trace":        "id 550e8400-e29b-41d4-a716-446655440000",
			"trip_plan_id": "nested-trip",
		},
		"items": []map[string]any{
			{
				"trace":        "id 550e8400-e29b-41d4-a716-446655440000",
				"trip_plan_id": "list-trip",
			},
		},
	}
	originalReason := "trace 550e8400-e29b-41d4-a716-446655440000"
	got := r.Run(t, scriptedMetric{
		name: "X",
		result: Result{
			Score:  1,
			Passed: true,
			Metric: "X",
			Reason: originalReason,
			Dimensions: []DimensionResult{
				{Name: "d", Score: 1, Passed: true, Reason: originalReason},
			},
			Metadata: originalMetadata,
		},
	}, Case{})

	written := sink.last()
	if written.Reason != "trace [REDACTED_UUID]" {
		t.Fatalf("reason was not redacted: %q", written.Reason)
	}
	if written.Dimensions[0].Reason != "trace [REDACTED_UUID]" {
		t.Fatalf("dimension reason was not redacted: %+v", written.Dimensions[0])
	}
	if written.Metadata["trip_plan_id"] != "[REDACTED]" {
		t.Fatalf("field was not redacted: %+v", written.Metadata)
	}
	nested, ok := written.Metadata["nested"].(map[string]any)
	if !ok || nested["trace"] != "id [REDACTED_UUID]" || nested["trip_plan_id"] != "[REDACTED]" {
		t.Fatalf("nested metadata was not redacted: %+v", written.Metadata)
	}
	items, ok := written.Metadata["items"].([]map[string]any)
	if !ok ||
		len(items) != 1 ||
		items[0]["trace"] != "id [REDACTED_UUID]" ||
		items[0]["trip_plan_id"] != "[REDACTED]" {
		t.Fatalf("metadata list was not redacted: %+v", written.Metadata)
	}
	if got.Reason != originalReason ||
		got.Metadata["trip_plan_id"] != "trip-123" ||
		originalMetadata["trip_plan_id"] != "trip-123" {
		t.Fatalf("redaction mutated original data: got=%+v original=%+v", got, originalMetadata)
	}
}

func TestRunScenario_RedactsScenarioSummary(t *testing.T) {
	t.Setenv(EnvVar, "1")

	sink := &recordingSink{}
	r := NewRunner(
		&MockJudge{},
		WithResultSink(sink),
		WithRedactors(UUIDRedactor(), FieldRedactor("trip_plan_id")),
	)
	got := r.RunScenario(t, Scenario{
		Name:     "redacted_summary",
		Metadata: map[string]any{"trip_plan_id": "trip-123"},
		Driver: func(ctx context.Context, req StepRequest) (StepResult, error) {
			return StepResult{
				Turns: []Turn{{ToolCalls: []ToolCall{{Name: "lookup-550e8400-e29b-41d4-a716-446655440000"}}}},
				Artifacts: map[string]json.RawMessage{
					"artifact-550e8400-e29b-41d4-a716-446655440000": json.RawMessage(`{"ok":true}`),
				},
			}, nil
		},
		Steps: []Step{{Name: "one"}},
	})
	if !got.Passed {
		t.Fatalf("expected scenario pass, got %+v", got)
	}

	summary := sink.last()
	if summary.Kind != runResultKindScenarioSummary || summary.ScenarioSummary == nil {
		t.Fatalf("expected scenario summary row, got %+v", summary)
	}
	if summary.ScenarioSummary.Metadata["trip_plan_id"] != "[REDACTED]" {
		t.Fatalf("summary metadata was not redacted: %+v", summary.ScenarioSummary.Metadata)
	}
	step := summary.ScenarioSummary.Steps[0]
	if step.ToolCalls[0] != "lookup-[REDACTED_UUID]" ||
		step.ArtifactKeys[0] != "artifact-[REDACTED_UUID]" {
		t.Fatalf("summary strings were not redacted: %+v", step)
	}
}

func readRunResultsJSONL(t *testing.T, path string) []RunResult {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = f.Close()
	}()

	var rows []RunResult
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row RunResult
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("Unmarshal row: %v", err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return rows
}
