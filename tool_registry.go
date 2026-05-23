package eval

import (
	"errors"
	"fmt"
	"strings"
)

// ToolRegistry validates exact tool names for a scenario.
//
// The zero value is valid and disables registry checks.
type ToolRegistry struct {
	names map[string]struct{}
	err   error
}

// NewToolRegistry returns a registry containing the provided exact tool names.
func NewToolRegistry(names ...string) ToolRegistry {
	registry := ToolRegistry{
		names: make(map[string]struct{}, len(names)),
	}
	var errs []error
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("tool name is empty"))
			continue
		}
		if _, exists := registry.names[name]; exists {
			errs = append(errs, fmt.Errorf("duplicate tool name %q", name))
			continue
		}
		registry.names[name] = struct{}{}
	}
	if len(errs) > 0 {
		registry.err = errors.Join(errs...)
	}
	return registry
}

// Validate reports whether the registry configuration is usable.
func (tr ToolRegistry) Validate() error {
	return tr.err
}

func (tr ToolRegistry) configured() bool {
	return len(tr.names) > 0 || tr.err != nil
}

func (tr ToolRegistry) has(name string) bool {
	_, ok := tr.names[name]
	return ok
}
