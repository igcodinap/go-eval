// Package ollamaeval provides an Ollama-backed go-eval judge adapter.
package ollamaeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	eval "github.com/igcodinap/go-eval"
)

const (
	defaultBaseURL = "http://localhost:11434"
	defaultModel   = "llama3.2"
	defaultTimeout = 30 * time.Second
)

// Option configures a Judge at construction time.
type Option func(*Judge)

// Judge is an Ollama-backed go-eval judge adapter.
//
// It satisfies both eval.Judge and eval.RawJudge.
type Judge struct {
	client    *http.Client
	baseURL   string
	modelName string
	timeout   time.Duration
}

// NewJudge creates a Judge for the given Ollama model.
//
// If modelName is empty, the adapter uses llama3.2. By default, requests are
// sent to http://localhost:11434 with http.DefaultClient.
func NewJudge(modelName string, opts ...Option) *Judge {
	j := &Judge{
		client:    http.DefaultClient,
		baseURL:   defaultBaseURL,
		modelName: modelName,
		timeout:   defaultTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(j)
		}
	}
	return j
}

// WithBaseURL configures the Ollama server URL.
func WithBaseURL(baseURL string) Option {
	return func(j *Judge) {
		j.baseURL = baseURL
	}
}

// WithHTTPClient configures the HTTP client used for Ollama requests.
func WithHTTPClient(client *http.Client) Option {
	return func(j *Judge) {
		j.client = client
	}
}

// WithTimeout configures the per-call timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(j *Judge) {
		j.timeout = timeout
	}
}

// EvaluateRaw implements eval.RawJudge.
func (j *Judge) EvaluateRaw(ctx context.Context, prompt string) (eval.RawJudgeResponse, error) {
	if j == nil {
		return eval.RawJudgeResponse{}, errors.New("ollama judge is nil")
	}
	if j.client == nil {
		return eval.RawJudgeResponse{}, errors.New("ollama judge http client is nil")
	}

	endpoint, err := generateEndpoint(j.baseURL)
	if err != nil {
		return eval.RawJudgeResponse{}, err
	}

	timeout := j.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(generateRequest{
		Model:   defaultString(j.modelName, defaultModel),
		Prompt:  prompt,
		Stream:  false,
		Format:  "json",
		Options: generateOptions{Temperature: 0},
	})
	if err != nil {
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return eval.RawJudgeResponse{}, fmt.Errorf("ollama: POST /api/generate returned %s", resp.Status)
		}
		msg := strings.TrimSpace(string(payload))
		if msg == "" {
			return eval.RawJudgeResponse{}, fmt.Errorf("ollama: POST /api/generate returned %s", resp.Status)
		}
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: POST /api/generate returned %s: %s", resp.Status, msg)
	}

	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: decode response: %w", err)
	}
	if out.Error != "" {
		return eval.RawJudgeResponse{}, fmt.Errorf("ollama: %s", out.Error)
	}

	return eval.RawJudgeResponse{
		Content:          out.Response,
		Tokens:           out.PromptEvalCount + out.EvalCount,
		PromptTokens:     out.PromptEvalCount,
		CompletionTokens: out.EvalCount,
	}, nil
}

// Evaluate implements eval.Judge.
func (j *Judge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	raw, err := j.EvaluateRaw(ctx, prompt)
	if err != nil {
		return eval.JudgeResponse{}, err
	}

	parsed, parseErr := parseJudgeJSON(raw.Content)
	if parseErr == nil {
		return eval.JudgeResponse{
			Score:            *parsed.Score,
			Reason:           parsed.Reason,
			Tokens:           raw.Tokens,
			PromptTokens:     raw.PromptTokens,
			CompletionTokens: raw.CompletionTokens,
		}, nil
	}

	rawRetry, err := j.EvaluateRaw(ctx, prompt+"\n\nJSON only, no prose.")
	if err != nil {
		return eval.JudgeResponse{}, err
	}
	parsed, parseErr = parseJudgeJSON(rawRetry.Content)
	if parseErr != nil {
		return eval.JudgeResponse{}, fmt.Errorf("ollama judge malformed model response after retry: %w", parseErr)
	}

	return eval.JudgeResponse{
		Score:            *parsed.Score,
		Reason:           parsed.Reason,
		Tokens:           raw.Tokens + rawRetry.Tokens,
		PromptTokens:     raw.PromptTokens + rawRetry.PromptTokens,
		CompletionTokens: raw.CompletionTokens + rawRetry.CompletionTokens,
	}, nil
}

type generateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	Stream  bool            `json:"stream"`
	Format  string          `json:"format,omitempty"`
	Options generateOptions `json:"options,omitempty"`
}

type generateOptions struct {
	Temperature float64 `json:"temperature"`
}

type generateResponse struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	Error           string `json:"error,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
}

type judgeJSON struct {
	Score  *float64 `json:"score"`
	Reason string   `json:"reason"`
}

func parseJudgeJSON(s string) (judgeJSON, error) {
	candidate := eval.ExtractJSONObjectCandidate(s)

	var out judgeJSON
	if err := json.Unmarshal([]byte(candidate), &out); err != nil {
		return out, fmt.Errorf("invalid JSON response: %w", err)
	}
	if out.Score == nil {
		return out, errors.New(`missing "score" field`)
	}
	if math.IsNaN(*out.Score) || *out.Score < 0 || *out.Score > 1 {
		return out, fmt.Errorf("score %.4f out of range [0,1]", *out.Score)
	}

	return judgeJSON{
		Score:  out.Score,
		Reason: strings.TrimSpace(out.Reason),
	}, nil
}

func generateEndpoint(baseURL string) (string, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		raw = defaultBaseURL
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ollama: invalid base URL %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ollama: invalid base URL %q: must include scheme and host", raw)
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/api/generate"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
