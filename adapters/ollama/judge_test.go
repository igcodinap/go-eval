package ollamaeval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestParseJudgeJSON_Direct(t *testing.T) {
	out, err := parseJudgeJSON(`{"score":0.75,"reason":" ok "}`)
	if err != nil {
		t.Fatalf("parseJudgeJSON: %v", err)
	}
	if out.Score == nil || *out.Score != 0.75 || out.Reason != "ok" {
		t.Fatalf("unexpected parse result: %+v", out)
	}
}

func TestParseJudgeJSON_WithProse(t *testing.T) {
	out, err := parseJudgeJSON("Result:\n{\"score\":0.8,\"reason\":\"solid\"}\nDone.")
	if err != nil {
		t.Fatalf("parseJudgeJSON: %v", err)
	}
	if out.Score == nil || *out.Score != 0.8 {
		t.Fatalf("unexpected parse result: %+v", out)
	}
}

func TestParseJudgeJSON_MissingScore(t *testing.T) {
	if _, err := parseJudgeJSON(`{"reason":"ok"}`); err == nil {
		t.Fatalf("expected missing score error")
	}
}

func TestParseJudgeJSON_OutOfRange(t *testing.T) {
	if _, err := parseJudgeJSON(`{"score":1.2,"reason":"bad"}`); err == nil {
		t.Fatalf("expected out-of-range score error")
	}
}

func TestEvaluateRawPostsGenerateRequestAndCopiesTokenSplit(t *testing.T) {
	requests := make(chan generateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type: %s", got)
		}

		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- req

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(generateResponse{
			Response:        `{"score":0.9,"reason":"ok"}`,
			Done:            true,
			PromptEvalCount: 3,
			EvalCount:       5,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	j := NewJudge("llama3.2", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	resp, err := j.EvaluateRaw(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("EvaluateRaw: %v", err)
	}
	if resp.Content != `{"score":0.9,"reason":"ok"}` {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if resp.Tokens != 8 || resp.PromptTokens != 3 || resp.CompletionTokens != 5 {
		t.Fatalf("unexpected token fields: %+v", resp)
	}

	req := <-requests
	if req.Model != "llama3.2" || req.Prompt != "prompt" || req.Stream || req.Format != "json" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if req.Options.Temperature != 0 {
		t.Fatalf("unexpected options: %+v", req.Options)
	}
}

func TestEvaluateAggregatesRetryTokenSplit(t *testing.T) {
	j, calls := newStubJudge(t, []stubGenerate{
		{content: "not-json", promptTokens: 3, completionTokens: 2},
		{content: `{"score":0.85,"reason":"ok"}`, promptTokens: 7, completionTokens: 4},
	})

	resp, err := j.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected retry, got %d calls", got)
	}
	if resp.Score != 0.85 || resp.Tokens != 16 || resp.PromptTokens != 10 || resp.CompletionTokens != 6 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestEvaluateMalformedResponseReturnsClearError(t *testing.T) {
	j, calls := newStubJudge(t, []stubGenerate{
		{content: "not-json"},
		{content: `{"reason":"missing score"}`},
	})

	_, err := j.Evaluate(context.Background(), "prompt")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "malformed model response") {
		t.Fatalf("expected malformed response error, got: %v", err)
	}
	if !strings.Contains(err.Error(), `missing "score" field`) {
		t.Fatalf("expected parse detail, got: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected retry, got %d calls", got)
	}
}

func TestEvaluateRawHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	j := NewJudge("missing", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	_, err := j.EvaluateRaw(context.Background(), "prompt")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "404 Not Found") || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvaluateConcurrentUse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(generateResponse{
			Response:        `{"score":0.95,"reason":"ok"}`,
			Done:            true,
			PromptEvalCount: 2,
			EvalCount:       3,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	j := NewJudge("llama3.2", WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	const workers = 20
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			resp, err := j.Evaluate(context.Background(), "prompt")
			if err != nil {
				errs <- err
				return
			}
			if resp.Score != 0.95 || resp.Tokens != 5 {
				errs <- fmt.Errorf("unexpected response: %+v", resp)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != workers {
		t.Fatalf("expected %d calls, got %d", workers, got)
	}
}

type stubGenerate struct {
	content          string
	promptTokens     int
	completionTokens int
}

func newStubJudge(t *testing.T, responses []stubGenerate) (*Judge, *atomic.Int32) {
	t.Helper()
	if len(responses) == 0 {
		t.Fatal("newStubJudge requires at least one response")
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			msg := "unexpected path: " + r.URL.Path
			t.Error(msg)
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		idx := int(calls.Add(1) - 1)
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		response := responses[idx]

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(generateResponse{
			Response:        response.content,
			Done:            true,
			PromptEvalCount: response.promptTokens,
			EvalCount:       response.completionTokens,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return NewJudge("llama3.2", WithBaseURL(server.URL), WithHTTPClient(server.Client())), &calls
}
