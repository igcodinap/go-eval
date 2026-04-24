package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
	return s.results[len(s.results)-1]
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
		TestName: "t",
		Metric:   "m",
		Score:    1,
		Metadata: map[string]any{"suite": "conversation", "user_language": "spanish"},
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
		Score:    0.5,
		Passed:   true,
		Metric:   "MetricX",
		Reason:   "ok",
		Metadata: map[string]any{"trace_id": "abc"},
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
