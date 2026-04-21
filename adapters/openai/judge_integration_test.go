//go:build integration

package openaieval

import (
	"context"
	"os"
	"testing"
)

func TestJudgeIntegration_Evaluate(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}

	j, err := NewJudgeFromEnv("")
	if err != nil {
		t.Fatalf("NewJudgeFromEnv: %v", err)
	}

	resp, err := j.Evaluate(context.Background(), `Return ONLY {"score": 1.0, "reason":"ok"}`)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score < 0 || resp.Score > 1 {
		t.Fatalf("score out of range: %+v", resp)
	}
}
