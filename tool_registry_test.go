package eval

import (
	"strings"
	"testing"
)

func TestToolRegistryValidate(t *testing.T) {
	if err := NewToolRegistry("search", "plan_route").Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestToolRegistryValidateRejectsBadNames(t *testing.T) {
	tests := []struct {
		name string
		reg  ToolRegistry
		want string
	}{
		{name: "empty", reg: NewToolRegistry(""), want: "empty"},
		{name: "duplicate", reg: NewToolRegistry("search", "search"), want: "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestToolRegistryZeroValueIsOptional(t *testing.T) {
	var reg ToolRegistry
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if reg.configured() {
		t.Fatalf("zero registry should not be configured")
	}
}
