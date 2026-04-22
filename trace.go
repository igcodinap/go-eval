package eval

import (
	"context"
	"os"
)

// TraceEnvVar is the environment variable that enables judge I/O tracing.
// When set to any non-empty value, every judge prompt and response is
// logged via the testing.TB passed to Runner.Run.
const TraceEnvVar = "GOEVAL_TRACE"

type traceTB interface {
	Helper()
	Logf(format string, args ...any)
}

type tracingJudge struct {
	inner Judge
	tb    traceTB
}

func (t *tracingJudge) Evaluate(ctx context.Context, prompt string) (JudgeResponse, error) {
	t.tb.Helper()
	t.tb.Logf("[goeval-trace] prompt:\n%s", prompt)
	resp, err := t.inner.Evaluate(ctx, prompt)
	if err != nil {
		t.tb.Logf("[goeval-trace] error: %v", err)
		return resp, err
	}
	t.tb.Logf("[goeval-trace] response score=%.3f tokens=%d reason=%q",
		resp.Score, resp.Tokens, resp.Reason)
	return resp, nil
}

type tracingRawJudge struct {
	*tracingJudge
	raw RawJudge
}

func (t *tracingRawJudge) EvaluateRaw(ctx context.Context, prompt string) (RawJudgeResponse, error) {
	t.tb.Helper()
	t.tb.Logf("[goeval-trace] prompt:\n%s", prompt)
	resp, err := t.raw.EvaluateRaw(ctx, prompt)
	if err != nil {
		t.tb.Logf("[goeval-trace] error: %v", err)
		return resp, err
	}
	t.tb.Logf("[goeval-trace] raw response tokens=%d:\n%s", resp.Tokens, resp.Content)
	return resp, nil
}

func maybeTrace(j Judge, tb traceTB) Judge {
	if os.Getenv(TraceEnvVar) == "" {
		return j
	}
	base := &tracingJudge{inner: j, tb: tb}
	if raw, ok := j.(RawJudge); ok {
		return &tracingRawJudge{tracingJudge: base, raw: raw}
	}
	return base
}
