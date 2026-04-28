package openaieval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestParseJudgeJSON_Direct(t *testing.T) {
	out, err := parseJudgeJSON(`{"score":0.75,"reason":"ok"}`)
	if err != nil {
		t.Fatalf("parseJudgeJSON: %v", err)
	}
	if out.Score == nil || *out.Score != 0.75 || out.Reason != "ok" {
		t.Fatalf("unexpected parse result: %+v", out)
	}
}

func TestParseJudgeJSON_Fenced(t *testing.T) {
	out, err := parseJudgeJSON("```json\n{\"score\":0.9,\"reason\":\"great\"}\n```")
	if err != nil {
		t.Fatalf("parseJudgeJSON: %v", err)
	}
	if out.Score == nil || *out.Score != 0.9 || out.Reason != "great" {
		t.Fatalf("unexpected parse result: %+v", out)
	}
}

func TestParseJudgeJSON_WithProse(t *testing.T) {
	out, err := parseJudgeJSON("Here is the result:\n{\"score\":0.8,\"reason\":\"solid\"}\nThanks.")
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

func TestEvaluateRawCopiesTokenSplit(t *testing.T) {
	j, _ := newStubJudge(t, []stubCompletion{
		{content: `{"score":0.9,"reason":"ok"}`, promptTokens: 3, completionTokens: 5},
	})

	resp, err := j.EvaluateRaw(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("EvaluateRaw: %v", err)
	}
	if resp.Tokens != 8 || resp.PromptTokens != 3 || resp.CompletionTokens != 5 {
		t.Fatalf("unexpected token fields: %+v", resp)
	}
}

func TestEvaluateAggregatesRetryTokenSplit(t *testing.T) {
	j, calls := newStubJudge(t, []stubCompletion{
		{content: "not-json", promptTokens: 3, completionTokens: 2},
		{content: `{"score":0.85,"reason":"ok"}`, promptTokens: 7, completionTokens: 4},
	})

	resp, err := j.Evaluate(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("expected retry, got %d calls", *calls)
	}
	if resp.Score != 0.85 || resp.Tokens != 16 || resp.PromptTokens != 10 || resp.CompletionTokens != 6 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

type stubCompletion struct {
	content          string
	promptTokens     int
	completionTokens int
}

func newStubJudge(t *testing.T, completions []stubCompletion) (*Judge, *int) {
	t.Helper()
	if len(completions) == 0 {
		t.Fatal("newStubJudge requires at least one completion")
	}

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			msg := "unexpected path: " + r.URL.Path
			t.Error(msg)
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		idx := calls
		if idx >= len(completions) {
			idx = len(completions) - 1
		}
		calls++
		completion := completions[idx]

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 0,
			"model":   openai.GPT4oMini,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    openai.ChatMessageRoleAssistant,
						"content": completion.content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     completion.promptTokens,
				"completion_tokens": completion.completionTokens,
				"total_tokens":      completion.promptTokens + completion.completionTokens,
			},
		})
		if err != nil {
			msg := "encode response: " + err.Error()
			t.Error(msg)
			http.Error(w, msg, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	config := openai.DefaultConfig("test")
	config.BaseURL = server.URL + "/v1"
	return NewJudge(openai.NewClientWithConfig(config), openai.GPT4oMini), &calls
}
