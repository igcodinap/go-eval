package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type rawJudgeFunc struct {
	mu    sync.Mutex
	calls int
	fn    func(context.Context, string) (RawJudgeResponse, error)
}

func (j *rawJudgeFunc) EvaluateRaw(ctx context.Context, prompt string) (RawJudgeResponse, error) {
	j.mu.Lock()
	j.calls++
	j.mu.Unlock()
	return j.fn(ctx, prompt)
}

func (j *rawJudgeFunc) Calls() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.calls
}

func TestJudgeExecutorParsesJSONJudgeResponse(t *testing.T) {
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{
				Content:          "```json\n{\"score\":0.82,\"reason\":\"solid\"}\n```",
				Tokens:           9,
				PromptTokens:     4,
				CompletionTokens: 5,
			}, nil
		},
	}
	executor := NewJudgeExecutor(raw)

	resp, err := executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 0.82 || resp.Reason != "solid" || resp.Tokens != 9 || resp.PromptTokens != 4 || resp.CompletionTokens != 5 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestJudgeExecutorRetriesParseFailureAndCachesSuccess(t *testing.T) {
	call := 0
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			call++
			if call == 1 {
				return RawJudgeResponse{Content: "not json", Tokens: 1}, nil
			}
			return RawJudgeResponse{Content: `{"score":1,"reason":"ok"}`, Tokens: 2}, nil
		},
	}
	cache := NewInMemoryJudgeCache()
	executor := NewJudgeExecutor(raw, WithJudgeExecutorAttempts(2), WithJudgeCache(cache))

	resp, err := executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate first: %v", err)
	}
	if resp.Score != 1 || raw.Calls() != 2 {
		t.Fatalf("first response/calls = %+v/%d", resp, raw.Calls())
	}

	resp, err = executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate cached: %v", err)
	}
	if resp.Score != 1 || raw.Calls() != 2 {
		t.Fatalf("cached response/calls = %+v/%d", resp, raw.Calls())
	}
}

func TestJudgeExecutorDefaultCacheNamespaceIsPerExecutor(t *testing.T) {
	cache := NewInMemoryJudgeCache()
	first := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":0.1,"reason":"first"}`}, nil
		},
	}
	second := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":0.9,"reason":"second"}`}, nil
		},
	}

	firstExec := NewJudgeExecutor(first, WithJudgeCache(cache))
	secondExec := NewJudgeExecutor(second, WithJudgeCache(cache))

	firstResp, err := firstExec.Evaluate(context.Background(), "same prompt")
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	secondResp, err := secondExec.Evaluate(context.Background(), "same prompt")
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if firstResp.Score != 0.1 || secondResp.Score != 0.9 {
		t.Fatalf("cache namespaces collided: first=%+v second=%+v", firstResp, secondResp)
	}
	if first.Calls() != 1 || second.Calls() != 1 {
		t.Fatalf("raw calls = %d/%d, want 1/1", first.Calls(), second.Calls())
	}
}

func TestJudgeExecutorSharedCacheNamespaceIsExplicit(t *testing.T) {
	cache := NewInMemoryJudgeCache()
	first := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":0.1,"reason":"first"}`}, nil
		},
	}
	second := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":0.9,"reason":"second"}`}, nil
		},
	}

	firstExec := NewJudgeExecutor(first, WithJudgeCache(cache), WithJudgeCacheNamespace("shared"))
	secondExec := NewJudgeExecutor(second, WithJudgeCache(cache), WithJudgeCacheNamespace("shared"))

	if _, err := firstExec.Evaluate(context.Background(), "same prompt"); err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	secondResp, err := secondExec.Evaluate(context.Background(), "same prompt")
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if secondResp.Score != 0.1 {
		t.Fatalf("shared namespace did not reuse cached response: %+v", secondResp)
	}
	if second.Calls() != 0 {
		t.Fatalf("second raw calls = %d, want 0", second.Calls())
	}
}

func TestJudgeExecutorRetriesRawJudgeErrors(t *testing.T) {
	sentinel := errors.New("temporary")
	call := 0
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			call++
			if call == 1 {
				return RawJudgeResponse{}, sentinel
			}
			return RawJudgeResponse{Content: `{"score":0.7,"reason":"ok"}`}, nil
		},
	}
	executor := NewJudgeExecutor(raw, WithJudgeExecutorAttempts(2))

	resp, err := executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 0.7 || raw.Calls() != 2 {
		t.Fatalf("response/calls = %+v/%d", resp, raw.Calls())
	}
}

func TestJudgeExecutorDoesNotRetryCanceledContext(t *testing.T) {
	raw := &rawJudgeFunc{
		fn: func(ctx context.Context, _ string) (RawJudgeResponse, error) {
			return RawJudgeResponse{}, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewJudgeExecutor(raw, WithJudgeExecutorAttempts(3)).Evaluate(ctx, "prompt")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Evaluate err = %v, want context.Canceled", err)
	}
	if raw.Calls() != 1 {
		t.Fatalf("raw calls = %d, want 1", raw.Calls())
	}
}

func TestJudgeExecutorRetryPolicyCanDisableRetries(t *testing.T) {
	sentinel := errors.New("do not retry")
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{}, sentinel
		},
	}
	executor := NewJudgeExecutor(
		raw,
		WithJudgeExecutorAttempts(3),
		WithJudgeRetryPolicy(func(int, error) bool { return false }),
	)

	_, err := executor.Evaluate(context.Background(), "prompt")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Evaluate err = %v, want %v", err, sentinel)
	}
	if raw.Calls() != 1 {
		t.Fatalf("raw calls = %d, want 1", raw.Calls())
	}
}

func TestJudgeExecutorEvaluateRawBypassesParsedCache(t *testing.T) {
	call := 0
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			call++
			return RawJudgeResponse{Content: `{"score":0.5,"reason":"ok"}`}, nil
		},
	}
	executor := NewJudgeExecutor(raw, WithJudgeCache(NewInMemoryJudgeCache()))

	if _, err := executor.Evaluate(context.Background(), "prompt"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	rawResp, err := executor.EvaluateRaw(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("EvaluateRaw: %v", err)
	}
	if rawResp.Content == "" || call != 2 {
		t.Fatalf("EvaluateRaw appears cached: content=%q calls=%d", rawResp.Content, call)
	}
}

func TestJSONLJudgeEventSinkWritesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judge-events.jsonl")
	sink := NewJSONLJudgeEventSink(path)
	if err := sink.WriteJudgeEvent(JudgeEvent{PromptHash: "abc", Attempt: 1, ParseOK: true}); err != nil {
		t.Fatalf("WriteJudgeEvent: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"prompt_hash":"abc"`) {
		t.Fatalf("event JSONL missing prompt hash: %s", data)
	}
}

func TestJudgeExecutorEventSinkErrorsAreBestEffort(t *testing.T) {
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":1,"reason":"ok"}`}, nil
		},
	}
	executor := NewJudgeExecutor(raw, WithJudgeEventSink(errorJudgeEventSink{}))

	resp, err := executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 1 {
		t.Fatalf("Score = %v, want 1", resp.Score)
	}
}

func TestJudgeExecutorEventSinkPanicsAreBestEffort(t *testing.T) {
	raw := &rawJudgeFunc{
		fn: func(context.Context, string) (RawJudgeResponse, error) {
			return RawJudgeResponse{Content: `{"score":1,"reason":"ok"}`}, nil
		},
	}
	executor := NewJudgeExecutor(raw, WithJudgeEventSink(panicJudgeEventSink{}))

	resp, err := executor.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score != 1 {
		t.Fatalf("Score = %v, want 1", resp.Score)
	}
}

type errorJudgeEventSink struct{}

func (errorJudgeEventSink) WriteJudgeEvent(JudgeEvent) error {
	return errors.New("sink failed")
}

type panicJudgeEventSink struct{}

func (panicJudgeEventSink) WriteJudgeEvent(JudgeEvent) error {
	panic("sink failed")
}
