package eval

import "testing"

func TestBench_RunsMetricInBenchmark(t *testing.T) {
	t.Setenv("GOEVAL", "1")

	mj := &MockJudge{Response: JudgeResponse{Score: 0.95, Tokens: 42}}
	r := NewRunner(mj)

	res := testing.Benchmark(func(b *testing.B) {
		Bench(b, r, scriptedMetric{
			name:   "FakeBench",
			result: Result{Score: 0.95, Passed: true, Metric: "FakeBench", Tokens: 42},
		}, Case{})
	})

	if res.N == 0 {
		t.Fatalf("benchmark did not run any iterations")
	}
	if got := res.Extra["tokens/op"]; got == 0 {
		t.Fatalf("expected tokens/op metric to be reported, got 0")
	}
	if _, ok := res.Extra["score_mean"]; !ok {
		t.Fatalf("expected score_mean metric to be reported")
	}
}

func BenchmarkBenchHelper(b *testing.B) {
	b.Setenv("GOEVAL", "1")

	r := NewRunner(&MockJudge{Response: JudgeResponse{Score: 0.95, Tokens: 42}})
	Bench(b, r, scriptedMetric{
		name:   "FakeBench",
		result: Result{Score: 0.95, Passed: true, Metric: "FakeBench", Tokens: 42},
	}, Case{})
}
