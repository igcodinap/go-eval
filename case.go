// Package eval provides LLM evaluation primitives: cases, judges, metrics,
// and a Runner that ties them into the standard Go testing workflow.
package eval

import (
	"encoding/json"
	"time"
)

// Case is a single LLM evaluation input.
//
// Metrics read whichever fields they need (Input, Output, Expected, Context,
// Turns, ExpectedToolCalls) and ignore the rest. Metadata is user-defined: the
// library never interprets it; it travels with the Case for trace IDs, dataset
// provenance, and similar metadata. Artifacts holds named structured outputs
// for deterministic state checks; values are opaque to the library until a
// metric interprets them.
type Case struct {
	Input             string
	Output            string
	Expected          string
	Context           []string
	Turns             []Turn
	ExpectedToolCalls []ToolCall
	Metadata          map[string]any
	Artifacts         map[string]json.RawMessage
	Timeout           time.Duration
	_                 struct{}
}
