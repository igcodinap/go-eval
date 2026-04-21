// Package eval provides LLM evaluation primitives: cases, judges, metrics,
// and a Runner that ties them into the standard Go testing workflow.
package eval

// Case is a single LLM evaluation input.
//
// Metrics read whichever fields they need (Input, Output, Expected, Context)
// and ignore the rest. Metadata is user-defined: the library never reads it;
// it travels with the Case for trace IDs, dataset provenance, and similar
// metadata.
type Case struct {
	Input    string
	Output   string
	Expected string
	Context  []string
	Metadata map[string]any
}
