package openaieval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	eval "github.com/igcodinap/go-eval"
)

const (
	defaultTimeout = 30 * time.Second
	defaultModel   = openai.GPT4oMini
)

// Judge is an OpenAI-backed go-eval judge adapter.
//
// It satisfies both eval.Judge and eval.RawJudge.
type Judge struct {
	client    *openai.Client
	modelName string
	timeout   time.Duration
}

// NewJudge creates a Judge using the provided client and model name.
func NewJudge(client *openai.Client, modelName string) *Judge {
	return &Judge{
		client:    client,
		modelName: modelName,
		timeout:   defaultTimeout,
	}
}

// NewJudgeFromEnv creates a Judge using OPENAI_API_KEY.
func NewJudgeFromEnv(modelName string) (*Judge, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, errors.New("OPENAI_API_KEY not set")
	}
	return NewJudge(openai.NewClient(key), modelName), nil
}

// WithTimeout returns a copy of the judge with a different per-call timeout.
func (j *Judge) WithTimeout(timeout time.Duration) *Judge {
	if j == nil {
		return nil
	}
	cp := *j
	cp.timeout = timeout
	return &cp
}

// EvaluateRaw implements eval.RawJudge.
func (j *Judge) EvaluateRaw(ctx context.Context, prompt string) (eval.RawJudgeResponse, error) {
	if j == nil || j.client == nil {
		return eval.RawJudgeResponse{}, errors.New("openai judge client is nil")
	}

	timeout := j.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	model := j.modelName
	if model == "" {
		model = defaultModel
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := j.client.CreateChatCompletion(callCtx, openai.ChatCompletionRequest{
		Model:       model,
		Temperature: math.SmallestNonzeroFloat32,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})
	if err != nil {
		return eval.RawJudgeResponse{}, err
	}
	if len(resp.Choices) == 0 {
		return eval.RawJudgeResponse{}, errors.New("openai returned no choices")
	}

	return eval.RawJudgeResponse{
		Content: resp.Choices[0].Message.Content,
		Tokens:  resp.Usage.TotalTokens,
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
			Score:  *parsed.Score,
			Reason: parsed.Reason,
			Tokens: raw.Tokens,
		}, nil
	}

	// One retry with stricter output instructions.
	rawRetry, err := j.EvaluateRaw(ctx, prompt+"\n\nJSON only, no prose.")
	if err != nil {
		return eval.JudgeResponse{}, err
	}
	parsed, parseErr = parseJudgeJSON(rawRetry.Content)
	if parseErr != nil {
		return eval.JudgeResponse{}, fmt.Errorf("openai judge parse failed after retry: %w", parseErr)
	}

	return eval.JudgeResponse{
		Score:  *parsed.Score,
		Reason: parsed.Reason,
		Tokens: raw.Tokens + rawRetry.Tokens,
	}, nil
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
	if *out.Score < 0 || *out.Score > 1 {
		return out, fmt.Errorf("score %.4f out of range [0,1]", *out.Score)
	}
	outNormalized := judgeJSON{
		Score:  out.Score,
		Reason: strings.TrimSpace(out.Reason),
	}
	return outNormalized, nil
}
