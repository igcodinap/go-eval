package eval

import "context"

// Judge is an LLM-as-judge provider abstraction.
//
// Implementations must be safe for concurrent use because Runner instances
// are expected to be shared across `t.Parallel` subtests and benchmarks.
type Judge interface {
	Evaluate(ctx context.Context, prompt string) (JudgeResponse, error)
}

// RawJudge is an optional extension for judges that can return raw model text.
//
// Metrics that need structured multi-field parsing (for example Compound)
// can require this interface while standard metrics continue using Judge.
type RawJudge interface {
	EvaluateRaw(ctx context.Context, prompt string) (RawJudgeResponse, error)
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

// RawJudgeResponse is the raw model output for an evaluation prompt.
type RawJudgeResponse struct {
	Content string
	Tokens  int
}
