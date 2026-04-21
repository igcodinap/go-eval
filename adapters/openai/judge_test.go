package openaieval

import "testing"

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
