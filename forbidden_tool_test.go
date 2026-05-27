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

func TestForbiddenToolPatternAndExcept(t *testing.T) {
	c := caseWithCalls([]ToolCall{{Name: "route_preview"}, {Name: "route_plan"}}, nil)

	result, err := (ForbiddenTool{
		Patterns: []string{"route_*"},
		Except:   []string{"route_preview"},
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "route_plan") {
		t.Fatalf("expected pattern failure outside exception, got %+v", result)
	}
}

func TestForbiddenToolExceptDoesNotOverrideExactName(t *testing.T) {
	c := caseWithCalls([]ToolCall{{Name: "delete_user"}}, nil)

	result, err := (ForbiddenTool{
		Names:  []string{"delete_user"},
		Except: []string{"delete_user"},
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected exact forbidden name to fail despite Except, got %+v", result)
	}
}

func TestForbiddenToolInvalidPatternFailsMetric(t *testing.T) {
	c := caseWithCalls([]ToolCall{{Name: "route_plan"}}, nil)

	result, err := (ForbiddenTool{Patterns: []string{"route_["}}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "invalid tool pattern") {
		t.Fatalf("expected invalid pattern failure, got %+v", result)
	}
}
