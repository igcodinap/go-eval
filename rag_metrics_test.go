package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextRecallRendersPromptAndPasses(t *testing.T) {
	judge := &MockJudge{Response: JudgeResponse{Score: 0.8, Reason: "context contains answer"}}
	result, err := (ContextRecall{Threshold: 0.8}).Score(context.Background(), judge, Case{
		Input:    "Where is the Eiffel Tower?",
		Expected: "Paris",
		Context:  []string{"The Eiffel Tower is in Paris."},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Metric != "ContextRecall" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if prompt := judge.LastPrompt(); !strings.Contains(prompt, "Paris") || !strings.Contains(prompt, "The Eiffel Tower") {
		t.Fatalf("prompt missing expected/context:\n%s", prompt)
	}
}

func TestContextRecallFailsWithoutContextBeforeJudge(t *testing.T) {
	judge := &MockJudge{}
	result, err := (ContextRecall{}).Score(context.Background(), judge, Case{Expected: "Paris"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Reason != "context is empty" || judge.Calls() != 0 {
		t.Fatalf("unexpected result/calls: %+v calls=%d", result, judge.Calls())
	}
}

func TestAnswerCorrectnessDefaultThresholdAndJudgeError(t *testing.T) {
	result, err := (AnswerCorrectness{}).Score(context.Background(), &MockJudge{
		Response: JudgeResponse{Score: 0.7, Reason: "correct"},
	}, Case{Input: "2+2", Expected: "4", Output: "Four"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected pass at default threshold, got %+v", result)
	}

	_, err = (AnswerCorrectness{}).Score(context.Background(), &MockJudge{Err: errors.New("offline")}, Case{Expected: "4"})
	if err == nil || !strings.Contains(err.Error(), "answer_correctness: judge") {
		t.Fatalf("expected judge error, got %v", err)
	}
}

func TestAnswerCorrectnessFailsWithoutExpected(t *testing.T) {
	result, err := (AnswerCorrectness{}).Score(context.Background(), &MockJudge{}, Case{Output: "anything"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Reason != "expected answer is empty" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestNoiseSensitivityRendersDistractors(t *testing.T) {
	judge := &MockJudge{Response: JudgeResponse{Score: 0.9, Reason: "ignored noise"}}
	result, err := (NoiseSensitivity{Threshold: 0.8}).Score(context.Background(), judge, Case{
		Input:    "Capital of France?",
		Expected: "Paris",
		Output:   "Paris",
		Context: []string{
			"Paris is the capital of France.",
			"Berlin is the capital of Germany.",
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("unexpected result: %+v", result)
	}
	if prompt := judge.LastPrompt(); !strings.Contains(prompt, "Berlin") || !strings.Contains(prompt, "distractors") {
		t.Fatalf("prompt missing noise context:\n%s", prompt)
	}
}
