package eval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type fakeTraceTB struct {
	mu   sync.Mutex
	logs []string
}

func (f *fakeTraceTB) Helper() {}
func (f *fakeTraceTB) Logf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

type mockRawJudge struct {
	Response   RawJudgeResponse
	ParsedResp JudgeResponse
	Err        error
}

func (m *mockRawJudge) EvaluateRaw(_ context.Context, _ string) (RawJudgeResponse, error) {
	if m.Err != nil {
		return RawJudgeResponse{}, m.Err
	}
	return m.Response, nil
}

func (m *mockRawJudge) Evaluate(_ context.Context, _ string) (JudgeResponse, error) {
	if m.Err != nil {
		return JudgeResponse{}, m.Err
	}
	return m.ParsedResp, nil
}

func TestMaybeTrace_EnvUnset_ReturnsIdentity(t *testing.T) {
	t.Setenv(TraceEnvVar, "")
	j := &MockJudge{}
	tb := &fakeTraceTB{}

	got := maybeTrace(j, tb)

	if got != j {
		t.Fatal("maybeTrace should return the same pointer when env is unset")
	}
}

func TestMaybeTrace_EnvSet_PlainJudge(t *testing.T) {
	t.Setenv(TraceEnvVar, "1")

	j := &MockJudge{
		Response: JudgeResponse{Score: 0.9, Reason: "good", Tokens: 10},
	}
	tb := &fakeTraceTB{}

	wrapped := maybeTrace(j, tb)
	resp, err := wrapped.Evaluate(context.Background(), "hello prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Score != 0.9 {
		t.Fatalf("expected score 0.9, got %.3f", resp.Score)
	}

	if len(tb.logs) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %v", len(tb.logs), tb.logs)
	}
	if tb.logs[0] != "[goeval-trace] prompt:\nhello prompt" {
		t.Fatalf("unexpected prompt log: %q", tb.logs[0])
	}
	if tb.logs[1] != `[goeval-trace] response score=0.900 tokens=10 reason="good"` {
		t.Fatalf("unexpected response log: %q", tb.logs[1])
	}
}

func TestMaybeTrace_EnvSet_RawJudge(t *testing.T) {
	t.Setenv(TraceEnvVar, "1")

	raw := &mockRawJudge{
		Response:   RawJudgeResponse{Content: `{"faithfulness": {"score": 0.8}}`, Tokens: 5},
		ParsedResp: JudgeResponse{Score: 0.8},
	}
	tb := &fakeTraceTB{}

	wrapped := maybeTrace(raw, tb)

	// Type assertion must still succeed
	_, ok := wrapped.(RawJudge)
	if !ok {
		t.Fatal("wrapped judge should still satisfy RawJudge type assertion")
	}

	resp, err := wrapped.(RawJudge).EvaluateRaw(context.Background(), "raw prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != `{"faithfulness": {"score": 0.8}}` {
		t.Fatalf("unexpected content: %q", resp.Content)
	}

	if len(tb.logs) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %v", len(tb.logs), tb.logs)
	}
	if tb.logs[0] != "[goeval-trace] prompt:\nraw prompt" {
		t.Fatalf("unexpected prompt log: %q", tb.logs[0])
	}
	if tb.logs[1] != "[goeval-trace] raw response tokens=5:\n{\"faithfulness\": {\"score\": 0.8}}" {
		t.Fatalf("unexpected raw response log: %q", tb.logs[1])
	}
}

func TestMaybeTrace_ErrorPath(t *testing.T) {
	t.Setenv(TraceEnvVar, "1")

	sentinel := errors.New("boom")
	j := &MockJudge{Err: sentinel}
	tb := &fakeTraceTB{}

	wrapped := maybeTrace(j, tb)
	_, err := wrapped.Evaluate(context.Background(), "fail prompt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}

	if len(tb.logs) != 2 {
		t.Fatalf("expected 2 log lines (prompt + error), got %d: %v", len(tb.logs), tb.logs)
	}
	if tb.logs[1] != "[goeval-trace] error: boom" {
		t.Fatalf("unexpected error log: %q", tb.logs[1])
	}
}

func TestMaybeTrace_SetenvMidTest(t *testing.T) {
	t.Setenv(TraceEnvVar, "")
	j := &MockJudge{}
	tb := &fakeTraceTB{}

	// Env unset → identity
	got := maybeTrace(j, tb)
	if got != j {
		t.Fatal("expected identity when env unset")
	}

	// Set env → wrapped
	t.Setenv(TraceEnvVar, "1")
	got2 := maybeTrace(j, tb)
	if got2 == j {
		t.Fatal("expected wrapped judge when env set")
	}
}
