// Package main provides a reference implementation of the go-eval Judge
// interface backed by the OpenAI API.
//
// This file is intentionally not part of the core go-eval module: the OpenAI
// SDK is a dependency most users should not inherit unless they want it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	openai "github.com/sashabaranov/go-openai"

	eval "github.com/igcodinap/go-eval"
)

// OpenAIJudge implements eval.Judge by calling OpenAI's chat completions API.
//
// Safe for concurrent use: the underlying openai.Client is safe to share.
// On JSON parse failure from the model, Evaluate retries with a stricter
// reminder to return JSON only.
type OpenAIJudge struct {
	Client     *openai.Client
	Model      string
	MaxRetries int
	Timeout    time.Duration
}

// NewOpenAIJudge constructs a Judge using OPENAI_API_KEY from the environment.
func NewOpenAIJudge(model string) (*OpenAIJudge, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, errors.New("OPENAI_API_KEY not set")
	}

	return &OpenAIJudge{
		Client:     openai.NewClient(key),
		Model:      model,
		MaxRetries: 2,
		Timeout:    30 * time.Second,
	}, nil
}

// Evaluate implements eval.Judge.
func (j *OpenAIJudge) Evaluate(ctx context.Context, prompt string) (eval.JudgeResponse, error) {
	retries := j.MaxRetries
	if retries < 1 {
		retries = 1
	}

	attemptPrompt := prompt
	var lastErr error

	for attempt := 0; attempt < retries; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, j.Timeout)
		resp, err := j.Client.CreateChatCompletion(callCtx, openai.ChatCompletionRequest{
			Model:       j.Model,
			Temperature: 0,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleUser, Content: attemptPrompt},
			},
		})
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if len(resp.Choices) == 0 {
			lastErr = errors.New("openai returned no choices")
			continue
		}

		parsed, parseErr := parseJudgeJSON(resp.Choices[0].Message.Content)
		if parseErr != nil {
			lastErr = parseErr
			attemptPrompt = prompt + "\n\nReminder: return ONLY a JSON object matching {\"score\": number, \"reason\": string}. No prose."
			continue
		}

		return eval.JudgeResponse{
			Score:  parsed.Score,
			Reason: parsed.Reason,
			Tokens: resp.Usage.TotalTokens,
		}, nil
	}

	return eval.JudgeResponse{}, fmt.Errorf("openai judge failed after %d attempts: %w", retries, lastErr)
}

type judgeJSON struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

var jsonBlockRE = regexp.MustCompile(`(?s)\{.*\}`)

func parseJudgeJSON(s string) (judgeJSON, error) {
	var out judgeJSON
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return out, nil
	}
	if match := jsonBlockRE.FindString(s); match != "" {
		if err := json.Unmarshal([]byte(match), &out); err == nil {
			return out, nil
		}
	}
	return out, fmt.Errorf("no JSON object in response: %q", s)
}
