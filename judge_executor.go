package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const defaultJudgeExecutorAttempts = 1

var judgeExecutorSequence uint64

// JudgeParser parses a raw judge response into the standard JudgeResponse
// contract used by existing metrics.
type JudgeParser interface {
	ParseJudgeResponse(RawJudgeResponse) (JudgeResponse, error)
}

// JudgeParserFunc adapts a function into a JudgeParser.
type JudgeParserFunc func(RawJudgeResponse) (JudgeResponse, error)

// ParseJudgeResponse implements JudgeParser.
func (f JudgeParserFunc) ParseJudgeResponse(resp RawJudgeResponse) (JudgeResponse, error) {
	if f == nil {
		return JudgeResponse{}, errors.New("nil judge parser")
	}
	return f(resp)
}

// JSONJudgeParser parses judge responses shaped like {"score": 0.82, "reason": "..."}.
//
// The parser accepts a surrounding markdown code fence and can extract the
// first JSON object from mixed prose using the same best-effort helper used by
// Compound.
type JSONJudgeParser struct{}

// ParseJudgeResponse implements JudgeParser.
func (p JSONJudgeParser) ParseJudgeResponse(raw RawJudgeResponse) (JudgeResponse, error) {
	candidate := ExtractJSONObjectCandidate(raw.Content)
	var payload struct {
		Score  *float64 `json:"score"`
		Reason string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return JudgeResponse{}, fmt.Errorf("invalid JSON judge response: %w", err)
	}
	if payload.Score == nil {
		return JudgeResponse{}, errors.New("judge response missing score")
	}
	if math.IsNaN(*payload.Score) || *payload.Score < 0 || *payload.Score > 1 {
		return JudgeResponse{}, fmt.Errorf("judge response score %.4f is outside [0,1]", *payload.Score)
	}
	return JudgeResponse{
		Score:            *payload.Score,
		Reason:           payload.Reason,
		Tokens:           raw.Tokens,
		PromptTokens:     raw.PromptTokens,
		CompletionTokens: raw.CompletionTokens,
	}, nil
}

// JudgeCache stores parsed judge responses. Implementations must be safe for
// concurrent use.
type JudgeCache interface {
	Get(key string) (JudgeResponse, bool)
	Set(key string, response JudgeResponse)
}

// InMemoryJudgeCache is a process-local, concurrency-safe JudgeCache.
type InMemoryJudgeCache struct {
	mu      sync.Mutex
	entries map[string]JudgeResponse
}

// NewInMemoryJudgeCache returns an empty in-memory judge cache.
func NewInMemoryJudgeCache() *InMemoryJudgeCache {
	return &InMemoryJudgeCache{entries: map[string]JudgeResponse{}}
}

// Get implements JudgeCache.
func (c *InMemoryJudgeCache) Get(key string) (JudgeResponse, bool) {
	if c == nil {
		return JudgeResponse{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, ok := c.entries[key]
	return resp, ok
}

// Set implements JudgeCache.
func (c *InMemoryJudgeCache) Set(key string, response JudgeResponse) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]JudgeResponse{}
	}
	c.entries[key] = response
}

// JudgeCacheKeyFunc returns the cache key for a prompt.
type JudgeCacheKeyFunc func(prompt string) string

// DefaultJudgeCacheKey returns a stable hash for a judge prompt.
//
// JudgeExecutor namespaces this key by default before using it with a cache.
func DefaultJudgeCacheKey(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// JudgeRetryPromptFunc returns the prompt to use for retry attempts after the
// first attempt fails. attempt is one-based.
type JudgeRetryPromptFunc func(prompt string, attempt int, err error) string

// JudgeRetryPolicy reports whether a failed attempt should be retried.
//
// attempt is the one-based attempt that just failed.
type JudgeRetryPolicy func(attempt int, err error) bool

// JudgeEventSink receives best-effort judge executor attempt events.
//
// Implementations must be safe for concurrent use. JudgeExecutor intentionally
// ignores sink write errors and recovered panics so diagnostics cannot fail
// otherwise valid evals.
type JudgeEventSink interface {
	WriteJudgeEvent(JudgeEvent) error
}

// JudgeEvent is a JSONL-safe diagnostic record for one judge executor attempt.
type JudgeEvent struct {
	Timestamp  string `json:"timestamp"`
	PromptHash string `json:"prompt_hash,omitempty"`
	Attempt    int    `json:"attempt"`
	CacheHit   bool   `json:"cache_hit,omitempty"`
	Raw        bool   `json:"raw,omitempty"`
	ParseOK    bool   `json:"parse_ok,omitempty"`
	Error      string `json:"error,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
	LatencyNS  int64  `json:"latency_ns,omitempty"`
	_          struct{}
}

// JudgeExecutorOption configures a JudgeExecutor.
type JudgeExecutorOption func(*JudgeExecutor)

// WithJudgeParser configures the parser used by Evaluate.
func WithJudgeParser(parser JudgeParser) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if parser != nil {
			e.parser = parser
		}
	}
}

// WithJudgeExecutorAttempts configures the maximum attempts for raw judge calls.
func WithJudgeExecutorAttempts(n int) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if n > 0 {
			e.maxAttempts = n
		}
	}
}

// WithJudgeExecutorRetryBackoff configures the delay between attempts.
func WithJudgeExecutorRetryBackoff(d time.Duration) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if d >= 0 {
			e.retryBackoff = d
		}
	}
}

// WithJudgeExecutorConcurrency limits concurrent raw judge calls when n > 0.
func WithJudgeExecutorConcurrency(n int) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if n > 0 {
			e.semaphore = make(chan struct{}, n)
		}
	}
}

// WithJudgeCache configures parsed-response caching for Evaluate.
func WithJudgeCache(cache JudgeCache) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		e.cache = cache
	}
}

// WithJudgeCacheKeyFunc configures how cache keys and prompt hashes are built.
func WithJudgeCacheKeyFunc(fn JudgeCacheKeyFunc) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if fn != nil {
			e.cacheKey = fn
		}
	}
}

// WithJudgeCacheNamespace configures a namespace for parsed-response cache
// entries.
//
// By default, each JudgeExecutor uses a unique namespace so shared caches do not
// accidentally mix results from different judges, parsers, or retry policies.
// Set the same namespace on multiple executors only when they are intentionally
// allowed to share parsed judge responses.
func WithJudgeCacheNamespace(namespace string) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if namespace != "" {
			e.cacheNamespace = namespace
		}
	}
}

// WithJudgeRetryPrompt configures retry prompts. A nil return value is not
// possible, so returning an empty string intentionally retries with empty input.
func WithJudgeRetryPrompt(fn JudgeRetryPromptFunc) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		e.retryPrompt = fn
	}
}

// WithJudgeRetryPolicy configures which failed attempts may be retried.
//
// A nil policy leaves the default behavior: context cancellation and deadline
// errors are not retried; other failures may retry until the attempt limit.
func WithJudgeRetryPolicy(fn JudgeRetryPolicy) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		if fn != nil {
			e.retryPolicy = fn
		}
	}
}

// WithJudgeEventSink configures a sink for judge attempt diagnostics.
func WithJudgeEventSink(sink JudgeEventSink) JudgeExecutorOption {
	return func(e *JudgeExecutor) {
		e.eventSink = sink
	}
}

// JudgeExecutor wraps a RawJudge with parsing, retry, concurrency, cache, and
// diagnostic behavior while preserving the existing Judge and RawJudge APIs.
type JudgeExecutor struct {
	raw            RawJudge
	parser         JudgeParser
	maxAttempts    int
	retryBackoff   time.Duration
	semaphore      chan struct{}
	cache          JudgeCache
	cacheKey       JudgeCacheKeyFunc
	cacheNamespace string
	retryPrompt    JudgeRetryPromptFunc
	retryPolicy    JudgeRetryPolicy
	eventSink      JudgeEventSink
}

// NewJudgeExecutor returns an executor-backed judge.
func NewJudgeExecutor(raw RawJudge, opts ...JudgeExecutorOption) *JudgeExecutor {
	e := &JudgeExecutor{
		raw:         raw,
		parser:      JSONJudgeParser{},
		maxAttempts: defaultJudgeExecutorAttempts,
		cacheKey:    DefaultJudgeCacheKey,
		retryPolicy: defaultJudgeRetryPolicy,
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.parser == nil {
		e.parser = JSONJudgeParser{}
	}
	if e.maxAttempts <= 0 {
		e.maxAttempts = defaultJudgeExecutorAttempts
	}
	if e.cacheKey == nil {
		e.cacheKey = DefaultJudgeCacheKey
	}
	if e.retryPolicy == nil {
		e.retryPolicy = defaultJudgeRetryPolicy
	}
	if e.cacheNamespace == "" {
		e.cacheNamespace = newJudgeExecutorNamespace(e)
	}
	return e
}

// Evaluate implements Judge.
func (e *JudgeExecutor) Evaluate(ctx context.Context, prompt string) (JudgeResponse, error) {
	if e == nil {
		return JudgeResponse{}, errors.New("nil judge executor")
	}
	if e.raw == nil {
		return JudgeResponse{}, errors.New("judge executor raw judge is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	promptHash := e.cacheKey(prompt)
	key := e.namespacedCacheKey(promptHash)
	if e.cache != nil {
		if resp, ok := e.cache.Get(key); ok {
			e.writeEvent(JudgeEvent{
				Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
				PromptHash: promptHash,
				Attempt:    0,
				CacheHit:   true,
				ParseOK:    true,
			})
			return resp, nil
		}
	}

	start := time.Now()
	var total JudgeResponse
	var lastErr error
	attemptPrompt := prompt
	for attempt := 1; attempt <= e.maxAttempts; attempt++ {
		rawResp, rawErr := e.evaluateRawOnce(ctx, attemptPrompt)
		total.Tokens += rawResp.Tokens
		total.PromptTokens += rawResp.PromptTokens
		total.CompletionTokens += rawResp.CompletionTokens

		if rawErr != nil {
			lastErr = rawErr
			e.writeAttemptEvent(promptHash, attempt, false, rawResp, time.Since(start), rawErr)
		} else {
			parsed, parseErr := e.parser.ParseJudgeResponse(rawResp)
			parsed.Tokens = total.Tokens
			parsed.PromptTokens = total.PromptTokens
			parsed.CompletionTokens = total.CompletionTokens
			if parseErr == nil {
				e.writeAttemptEvent(promptHash, attempt, true, rawResp, time.Since(start), nil)
				if e.cache != nil {
					e.cache.Set(key, parsed)
				}
				return parsed, nil
			}
			lastErr = parseErr
			e.writeAttemptEvent(promptHash, attempt, false, rawResp, time.Since(start), parseErr)
		}

		if attempt < e.maxAttempts && e.shouldRetry(attempt, lastErr) {
			if err := sleepContext(ctx, e.retryBackoff); err != nil {
				return JudgeResponse{
					Tokens:           total.Tokens,
					PromptTokens:     total.PromptTokens,
					CompletionTokens: total.CompletionTokens,
				}, err
			}
			if e.retryPrompt != nil {
				attemptPrompt = e.retryPrompt(prompt, attempt+1, lastErr)
			}
			continue
		}
		break
	}

	if lastErr == nil {
		lastErr = errors.New("judge executor exhausted attempts")
	}
	return JudgeResponse{
		Tokens:           total.Tokens,
		PromptTokens:     total.PromptTokens,
		CompletionTokens: total.CompletionTokens,
	}, lastErr
}

// EvaluateRaw implements RawJudge with executor retry and concurrency behavior.
func (e *JudgeExecutor) EvaluateRaw(ctx context.Context, prompt string) (RawJudgeResponse, error) {
	if e == nil {
		return RawJudgeResponse{}, errors.New("nil judge executor")
	}
	if e.raw == nil {
		return RawJudgeResponse{}, errors.New("judge executor raw judge is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	promptHash := e.cacheKey(prompt)
	start := time.Now()
	var total RawJudgeResponse
	var lastErr error
	attemptPrompt := prompt
	for attempt := 1; attempt <= e.maxAttempts; attempt++ {
		resp, err := e.evaluateRawOnce(ctx, attemptPrompt)
		total.Tokens += resp.Tokens
		total.PromptTokens += resp.PromptTokens
		total.CompletionTokens += resp.CompletionTokens
		if err == nil {
			total.Content = resp.Content
			e.writeEvent(JudgeEvent{
				Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
				PromptHash: promptHash,
				Attempt:    attempt,
				Raw:        true,
				ParseOK:    true,
				Tokens:     resp.Tokens,
				LatencyNS:  int64(time.Since(start)),
			})
			return total, nil
		}
		lastErr = err
		e.writeEvent(JudgeEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
			PromptHash: promptHash,
			Attempt:    attempt,
			Raw:        true,
			Error:      err.Error(),
			Tokens:     resp.Tokens,
			LatencyNS:  int64(time.Since(start)),
		})

		if attempt < e.maxAttempts && e.shouldRetry(attempt, lastErr) {
			if sleepErr := sleepContext(ctx, e.retryBackoff); sleepErr != nil {
				return total, sleepErr
			}
			if e.retryPrompt != nil {
				attemptPrompt = e.retryPrompt(prompt, attempt+1, lastErr)
			}
			continue
		}
		break
	}
	if lastErr == nil {
		lastErr = errors.New("judge executor exhausted attempts")
	}
	return total, lastErr
}

func (e *JudgeExecutor) evaluateRawOnce(ctx context.Context, prompt string) (RawJudgeResponse, error) {
	if e.semaphore != nil {
		select {
		case e.semaphore <- struct{}{}:
			defer func() { <-e.semaphore }()
		case <-ctx.Done():
			return RawJudgeResponse{}, ctx.Err()
		}
	}
	return e.raw.EvaluateRaw(ctx, prompt)
}

func (e *JudgeExecutor) namespacedCacheKey(key string) string {
	if e.cacheNamespace == "" {
		return key
	}
	return e.cacheNamespace + ":" + key
}

func (e *JudgeExecutor) shouldRetry(attempt int, err error) bool {
	if err == nil {
		return false
	}
	if e.retryPolicy == nil {
		return defaultJudgeRetryPolicy(attempt, err)
	}
	return e.retryPolicy(attempt, err)
}

func defaultJudgeRetryPolicy(_ int, err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func newJudgeExecutorNamespace(e *JudgeExecutor) string {
	id := atomic.AddUint64(&judgeExecutorSequence, 1)
	return fmt.Sprintf(
		"executor=%d;raw=%T;parser=%T;attempts=%d;retry_prompt=%t",
		id,
		e.raw,
		e.parser,
		e.maxAttempts,
		e.retryPrompt != nil,
	)
}

func (e *JudgeExecutor) writeAttemptEvent(key string, attempt int, parseOK bool, resp RawJudgeResponse, latency time.Duration, err error) {
	event := JudgeEvent{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		PromptHash: key,
		Attempt:    attempt,
		ParseOK:    parseOK,
		Tokens:     resp.Tokens,
		LatencyNS:  int64(latency),
	}
	if err != nil {
		event.Error = err.Error()
	}
	e.writeEvent(event)
}

func (e *JudgeExecutor) writeEvent(event JudgeEvent) {
	if e == nil || e.eventSink == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = e.eventSink.WriteJudgeEvent(event)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type jsonlJudgeEventSink struct {
	path string
	mu   sync.Mutex
}

// NewJSONLJudgeEventSink returns a JSONL sink for judge executor attempt events.
func NewJSONLJudgeEventSink(path string) JudgeEventSink {
	if path == "" {
		return nil
	}
	return &jsonlJudgeEventSink{path: path}
}

// DefaultJudgeEventSink creates a JSONL sink from GOEVAL_RESULTS_DIR.
//
// Returns nil when the env var is unset.
func DefaultJudgeEventSink() JudgeEventSink {
	dir := os.Getenv(ResultsDirEnvVar)
	if dir == "" {
		return nil
	}
	return NewJSONLJudgeEventSink(filepath.Join(dir, "judge-events.jsonl"))
}

func (s *jsonlJudgeEventSink) WriteJudgeEvent(event JudgeEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	writeErr := enc.Encode(event)
	closeErr := f.Close()
	if writeErr != nil && closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
