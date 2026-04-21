package eval

import (
	"context"
	"time"
)

// Metric is the contract any LLM evaluation must satisfy.
//
// Implementations are expected to be stateless value types: configuration
// fields are read-only during Score, and the Runner provides the Judge
// at evaluation time.
type Metric interface {
	Name() string
	Score(ctx context.Context, j Judge, c Case) (Result, error)
}

// Result is what a Metric returns after scoring a Case.
type Result struct {
	Score   float64
	Reason  string
	Passed  bool
	Metric  string
	Latency time.Duration
	Tokens  int
}
