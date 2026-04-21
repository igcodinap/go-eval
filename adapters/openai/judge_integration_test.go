//go:build integration

package openaieval

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestJudgeIntegration_Evaluate(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}

	j, err := NewJudgeFromEnv("")
	if err != nil {
		t.Fatalf("NewJudgeFromEnv: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	resp, err := j.Evaluate(ctx, `Return ONLY {"score": 1.0, "reason":"ok"}`)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Score < 0 || resp.Score > 1 {
		t.Fatalf("score out of range: %+v", resp)
	}
}
