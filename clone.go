package eval

import "encoding/json"

func cloneTurns(turns []Turn) []Turn {
	if turns == nil {
		return nil
	}
	out := make([]Turn, len(turns))
	for i, turn := range turns {
		out[i] = Turn{
			Role:       turn.Role,
			Content:    turn.Content,
			Name:       turn.Name,
			ToolCallID: turn.ToolCallID,
			ToolCalls:  cloneToolCalls(turn.ToolCalls),
			Metadata:   cloneMetadata(turn.Metadata),
		}
	}
	return out
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	if calls == nil {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: cloneRawMessage(call.Arguments),
			Result:    call.Result,
			Error:     call.Error,
			Metadata:  cloneMetadata(call.Metadata),
		}
	}
	return out
}

func cloneArtifacts(artifacts map[string]json.RawMessage) map[string]json.RawMessage {
	if artifacts == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(artifacts))
	for key, value := range artifacts {
		out[key] = cloneRawMessage(value)
	}
	return out
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case json.RawMessage:
		return cloneRawMessage(v)
	case []byte:
		return append([]byte(nil), v...)
	case map[string]any:
		return cloneMetadata(v)
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneAny(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, item := range v {
			out[i] = cloneMetadata(item)
		}
		return out
	case []map[string]string:
		out := make([]map[string]string, len(v))
		for i, item := range v {
			cloned := make(map[string]string, len(item))
			for key, nested := range item {
				cloned[key] = nested
			}
			out[i] = cloned
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case []int:
		return append([]int(nil), v...)
	case []float64:
		return append([]float64(nil), v...)
	case []bool:
		return append([]bool(nil), v...)
	default:
		return v
	}
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneTracePtr(trace *Trace) *Trace {
	if trace == nil {
		return nil
	}
	cloned := cloneTrace(trace)
	return &cloned
}

func cloneTrace(trace *Trace) Trace {
	if trace == nil {
		return Trace{}
	}
	out := Trace{
		ID:           trace.ID,
		Name:         trace.Name,
		TestName:     trace.TestName,
		ScenarioName: trace.ScenarioName,
		StartedAt:    trace.StartedAt,
		EndedAt:      trace.EndedAt,
		DurationNS:   trace.DurationNS,
		Spans:        cloneSpans(trace.Spans),
		Artifacts:    cloneArtifactRecords(trace.Artifacts),
		StateDeltas:  cloneStateDeltas(trace.StateDeltas),
		Metadata:     cloneMetadata(trace.Metadata),
	}
	return out
}

func cloneSpans(spans []Span) []Span {
	if spans == nil {
		return nil
	}
	out := make([]Span, len(spans))
	for i, span := range spans {
		out[i] = Span{
			ID:         span.ID,
			ParentID:   span.ParentID,
			Name:       span.Name,
			Kind:       span.Kind,
			StartedAt:  span.StartedAt,
			EndedAt:    span.EndedAt,
			DurationNS: span.DurationNS,
			Input:      span.Input,
			Output:     span.Output,
			Error:      span.Error,
			Metadata:   cloneMetadata(span.Metadata),
		}
		if span.ToolCall != nil {
			toolCall := cloneToolCalls([]ToolCall{*span.ToolCall})[0]
			out[i].ToolCall = &toolCall
		}
	}
	return out
}

func cloneArtifactRecords(records []ArtifactRecord) []ArtifactRecord {
	if records == nil {
		return nil
	}
	out := make([]ArtifactRecord, len(records))
	for i, record := range records {
		out[i] = ArtifactRecord{
			Key:      record.Key,
			Name:     record.Name,
			MIMEType: record.MIMEType,
			URI:      record.URI,
			Value:    cloneRawMessage(record.Value),
			Metadata: cloneMetadata(record.Metadata),
		}
	}
	return out
}

func cloneStateDeltas(deltas []StateDelta) []StateDelta {
	if deltas == nil {
		return nil
	}
	out := make([]StateDelta, len(deltas))
	for i, delta := range deltas {
		out[i] = StateDelta{
			Key:      delta.Key,
			Before:   cloneRawMessage(delta.Before),
			After:    cloneRawMessage(delta.After),
			Metadata: cloneMetadata(delta.Metadata),
		}
	}
	return out
}
