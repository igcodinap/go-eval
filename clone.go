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
