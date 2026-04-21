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

// DimensionResult is the structured score output for one Compound dimension.
type DimensionResult struct {
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold"`
	Passed    bool    `json:"passed"`
	Reason    string  `json:"reason,omitempty"`
}

// Result is what a Metric returns after scoring a Case.
type Result struct {
	Score      float64
	Reason     string
	Passed     bool
	Metric     string
	Latency    time.Duration
	Tokens     int
	Dimensions []DimensionResult
	_          struct{}
}
