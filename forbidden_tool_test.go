package eval

import (
	"context"
	"strings"
	"testing"
)

func TestForbiddenToolFailsWhenForbiddenNameAppears(t *testing.T) {
	c := caseWithCalls([]ToolCall{{Name: "search"}, {Name: "delete_user"}}, nil)

	result, err := (ForbiddenTool{Names: []string{"delete_user", "charge_card"}}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || result.Score != 0 || !strings.Contains(result.Reason, "delete_user") {
		t.Fatalf("expected forbidden tool failure, got %+v", result)
	}
}

func TestForbiddenToolPassesWhenNoForbiddenNameAppears(t *testing.T) {
	c := caseWithCalls([]ToolCall{{Name: "search"}}, nil)

	result, err := (ForbiddenTool{Names: []string{"delete_user"}}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected pass, got %+v", result)
	}
}

func TestForbiddenToolEmptyConfigPasses(t *testing.T) {
	result, err := (ForbiddenTool{}).Score(context.Background(), nil, caseWithCalls([]ToolCall{{Name: "search"}}, nil))
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed || !strings.Contains(result.Reason, "no forbidden tool names configured") {
		t.Fatalf("expected empty config pass, got %+v", result)
	}
}
