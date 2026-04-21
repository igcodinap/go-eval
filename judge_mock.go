package eval

import (
	"context"
	"sync"
)

// MockJudge is a deterministic Judge for tests.
//
// Configure behavior in precedence order:
//  1. Func - if set, called for every Evaluate.
//  2. Err - if non-nil, returned as the error.
//  3. Response - returned verbatim.
//
// MockJudge is safe for concurrent use.
type MockJudge struct {
	Response JudgeResponse
	Err      error
	Func     func(ctx context.Context, prompt string) (JudgeResponse, error)

	mu         sync.Mutex
	calls      int
	lastPrompt string
	allPrompts []string
}

// Evaluate records the call and returns the configured response.
func (m *MockJudge) Evaluate(ctx context.Context, prompt string) (JudgeResponse, error) {
	m.mu.Lock()
	m.calls++
	m.lastPrompt = prompt
	m.allPrompts = append(m.allPrompts, prompt)
	fn := m.Func
	err := m.Err
	resp := m.Response
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, prompt)
	}
	if err != nil {
		return JudgeResponse{}, err
	}
	return resp, nil
}

// Calls returns the number of Evaluate calls seen so far.
func (m *MockJudge) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// LastPrompt returns the most recent prompt passed to Evaluate.
func (m *MockJudge) LastPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPrompt
}

// AllPrompts returns a copy of every prompt seen by Evaluate.
func (m *MockJudge) AllPrompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, len(m.allPrompts))
	copy(out, m.allPrompts)
	return out
}
