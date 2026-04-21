package eval

import "context"

// Judge is an LLM-as-judge provider abstraction.
//
// Implementations must be safe for concurrent use because Runner instances
// are expected to be shared across `t.Parallel` subtests and benchmarks.
type Judge interface {
	Evaluate(ctx context.Context, prompt string) (JudgeResponse, error)
}

// JudgeResponse is the parsed output of an LLM-as-judge call.
//
// The Judge is responsible for parsing a model response such as
// `{"score": 0.82, "reason": "..."}` into this struct.
type JudgeResponse struct {
	Score  float64
	Reason string
	Tokens int
}
