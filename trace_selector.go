package eval

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxTraceJSONLLineSize = 10 * 1024 * 1024

// TraceTextSelectorKind identifies a supported text selector over Trace.
type TraceTextSelectorKind string

const (
	TraceSelectNone            TraceTextSelectorKind = ""
	TraceSelectName            TraceTextSelectorKind = "trace.name"
	TraceSelectMetadata        TraceTextSelectorKind = "trace.metadata"
	TraceSelectFirstSpanInput  TraceTextSelectorKind = "span.first.input"
	TraceSelectFirstSpanOutput TraceTextSelectorKind = "span.first.output"
	TraceSelectLastSpanInput   TraceTextSelectorKind = "span.last.input"
	TraceSelectLastSpanOutput  TraceTextSelectorKind = "span.last.output"
	TraceSelectSpanInput       TraceTextSelectorKind = "span.input"
	TraceSelectSpanOutput      TraceTextSelectorKind = "span.output"
	TraceSelectArtifactValue   TraceTextSelectorKind = "artifact.value"
	TraceSelectStateAfter      TraceTextSelectorKind = "state.after"
)

// TraceTextSelector selects one text value from a Trace.
type TraceTextSelector struct {
	Kind TraceTextSelectorKind
	// Name matches Span.Name for span selectors.
	Name string
	// Key matches trace metadata keys, artifact keys, or state delta keys.
	Key string
	_   struct{}
}

// TraceName selects Trace.Name.
func TraceName() TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectName}
}

// TraceMetadata selects Trace.Metadata[key].
func TraceMetadata(key string) TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectMetadata, Key: key}
}

// FirstSpanInput selects the first span input.
func FirstSpanInput() TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectFirstSpanInput}
}

// FirstSpanOutput selects the first span output.
func FirstSpanOutput() TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectFirstSpanOutput}
}

// LastSpanInput selects the last span input.
func LastSpanInput() TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectLastSpanInput}
}

// LastSpanOutput selects the last span output.
func LastSpanOutput() TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectLastSpanOutput}
}

// SpanInput selects input from the first span named name.
func SpanInput(name string) TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectSpanInput, Name: name}
}

// SpanOutput selects output from the first span named name.
func SpanOutput(name string) TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectSpanOutput, Name: name}
}

// ArtifactValue selects the raw JSON value for an artifact key.
func ArtifactValue(key string) TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectArtifactValue, Key: key}
}

// StateAfter selects the raw JSON after value for a state delta key.
func StateAfter(key string) TraceTextSelector {
	return TraceTextSelector{Kind: TraceSelectStateAfter, Key: key}
}

// SelectText returns the selected text, whether it was found, and any selector error.
func (s TraceTextSelector) SelectText(trace Trace) (string, bool, error) {
	switch s.Kind {
	case TraceSelectNone:
		return "", false, nil
	case TraceSelectName:
		return trace.Name, trace.Name != "", nil
	case TraceSelectMetadata:
		value, ok := trace.Metadata[s.Key]
		if !ok || value == nil {
			return "", false, nil
		}
		return stringifySelectorValue(value)
	case TraceSelectFirstSpanInput:
		if len(trace.Spans) == 0 {
			return "", false, nil
		}
		return trace.Spans[0].Input, trace.Spans[0].Input != "", nil
	case TraceSelectFirstSpanOutput:
		if len(trace.Spans) == 0 {
			return "", false, nil
		}
		return trace.Spans[0].Output, trace.Spans[0].Output != "", nil
	case TraceSelectLastSpanInput:
		if len(trace.Spans) == 0 {
			return "", false, nil
		}
		value := trace.Spans[len(trace.Spans)-1].Input
		return value, value != "", nil
	case TraceSelectLastSpanOutput:
		if len(trace.Spans) == 0 {
			return "", false, nil
		}
		value := trace.Spans[len(trace.Spans)-1].Output
		return value, value != "", nil
	case TraceSelectSpanInput:
		span, ok := firstSpanByName(trace.Spans, s.Name)
		if !ok {
			return "", false, nil
		}
		return span.Input, span.Input != "", nil
	case TraceSelectSpanOutput:
		span, ok := firstSpanByName(trace.Spans, s.Name)
		if !ok {
			return "", false, nil
		}
		return span.Output, span.Output != "", nil
	case TraceSelectArtifactValue:
		for _, artifact := range trace.Artifacts {
			if artifact.Key == s.Key {
				return string(artifact.Value), len(artifact.Value) > 0, nil
			}
		}
		return "", false, nil
	case TraceSelectStateAfter:
		for _, delta := range trace.StateDeltas {
			if delta.Key == s.Key {
				return string(delta.After), len(delta.After) > 0, nil
			}
		}
		return "", false, nil
	default:
		return "", false, fmt.Errorf("unsupported trace selector kind %q", s.Kind)
	}
}

// MissingSelectorAction controls TraceCaseSelector behavior when a configured
// selector does not match.
type MissingSelectorAction string

const (
	MissingSelectorError MissingSelectorAction = ""
	MissingSelectorEmpty MissingSelectorAction = "empty"
)

// TraceCaseSelector maps selected Trace fields into a Case.
type TraceCaseSelector struct {
	Input     TraceTextSelector
	Output    TraceTextSelector
	Expected  TraceTextSelector
	Context   []TraceTextSelector
	OnMissing MissingSelectorAction
	_         struct{}
}

// CaseFromTrace builds a Case from trace selector matches.
func (s TraceCaseSelector) CaseFromTrace(trace Trace) (Case, error) {
	var c Case
	c.TraceID = trace.ID
	c.Trace = cloneTracePtr(&trace)
	c.Metadata = cloneMetadata(trace.Metadata)

	var err error
	if c.Input, err = s.selectRequired(trace, "input", s.Input); err != nil {
		return Case{}, err
	}
	if c.Output, err = s.selectRequired(trace, "output", s.Output); err != nil {
		return Case{}, err
	}
	if c.Expected, err = s.selectRequired(trace, "expected", s.Expected); err != nil {
		return Case{}, err
	}
	for i, selector := range s.Context {
		value, ok, selectErr := selector.SelectText(trace)
		if selectErr != nil {
			return Case{}, fmt.Errorf("context selector %d: %w", i, selectErr)
		}
		if !ok {
			if s.OnMissing == MissingSelectorEmpty {
				c.Context = append(c.Context, "")
				continue
			}
			return Case{}, fmt.Errorf("context selector %d did not match", i)
		}
		c.Context = append(c.Context, value)
	}
	return c, nil
}

func (s TraceCaseSelector) selectRequired(trace Trace, field string, selector TraceTextSelector) (string, error) {
	if selector.Kind == TraceSelectNone {
		return "", nil
	}
	value, ok, err := selector.SelectText(trace)
	if err != nil {
		return "", fmt.Errorf("%s selector: %w", field, err)
	}
	if !ok && s.OnMissing != MissingSelectorEmpty {
		return "", fmt.Errorf("%s selector did not match", field)
	}
	return value, nil
}

func firstSpanByName(spans []Span, name string) (Span, bool) {
	for _, span := range spans {
		if span.Name == name {
			return span, true
		}
	}
	return Span{}, false
}

func stringifySelectorValue(value any) (string, bool, error) {
	switch v := value.(type) {
	case string:
		return v, v != "", nil
	case json.RawMessage:
		return string(v), len(v) > 0, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false, err
		}
		return string(data), len(data) > 0, nil
	}
}

// ReadTraceJSONL reads Trace rows from a JSONL stream.
func ReadTraceJSONL(r io.Reader) ([]Trace, error) {
	if r == nil {
		return nil, errors.New("trace jsonl reader is nil")
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxTraceJSONLLineSize)

	var traces []Trace
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var trace Trace
		if err := json.Unmarshal(line, &trace); err != nil {
			return nil, fmt.Errorf("trace jsonl line %d: %w", lineNo, err)
		}
		traces = append(traces, trace)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trace jsonl: %w", err)
	}
	return traces, nil
}

// ReadTraceJSONLFile reads Trace rows from a traces.jsonl file.
func ReadTraceJSONLFile(path string) ([]Trace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	traces, readErr := ReadTraceJSONL(f)
	closeErr := f.Close()
	if readErr != nil && closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return traces, nil
}
