package eval

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Redactor rewrites a string value before a RunResult is written to a sink.
//
// The path identifies the field being redacted, for example "reason",
// "dimensions.0.reason", or "metadata.trip_plan_id".
type Redactor func(path string, value string) string

var uuidRedactorRE = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)

// WithRedactors configures redactors that run before result sink writes.
//
// Nil redactors are ignored. Redactors do not affect values returned from
// Runner.Run or Runner.RunScenario.
func WithRedactors(redactors ...Redactor) Option {
	return func(r *Runner) {
		for _, redactor := range redactors {
			if redactor != nil {
				r.redactors = append(r.redactors, redactor)
			}
		}
	}
}

// UUIDRedactor replaces UUID-looking substrings in redacted string fields.
func UUIDRedactor() Redactor {
	return func(_ string, value string) string {
		return uuidRedactorRE.ReplaceAllString(value, "[REDACTED_UUID]")
	}
}

// FieldRedactor replaces metadata string values whose final path segment
// matches field.
func FieldRedactor(field string) Redactor {
	return func(path string, value string) string {
		if field == "" || !isMetadataPath(path) || finalPathSegment(path) != field {
			return value
		}
		return "[REDACTED]"
	}
}

func (r *Runner) redactRunResult(result RunResult) RunResult {
	if len(r.redactors) == 0 {
		return result
	}

	out := result
	out.Reason = r.applyRedactors("reason", out.Reason)
	if len(result.Dimensions) > 0 {
		out.Dimensions = make([]DimensionResult, len(result.Dimensions))
		copy(out.Dimensions, result.Dimensions)
		for i := range out.Dimensions {
			path := "dimensions." + strconv.Itoa(i) + ".reason"
			out.Dimensions[i].Reason = r.applyRedactors(path, out.Dimensions[i].Reason)
		}
	}
	out.Metadata = redactMetadata(r.redactors, "metadata", result.Metadata)
	if result.ScenarioSummary != nil {
		out.ScenarioSummary = redactScenarioSummary(r.redactors, result.ScenarioSummary)
	}
	return out
}

func (r *Runner) applyRedactors(path string, value string) string {
	for _, redactor := range r.redactors {
		value = redactor(path, value)
	}
	return value
}

func (r *Runner) redactTrace(trace Trace) Trace {
	if len(r.redactors) == 0 {
		return trace
	}
	out := trace
	out.Name = r.applyRedactors("trace.name", out.Name)
	out.TestName = r.applyRedactors("trace.test_name", out.TestName)
	out.ScenarioName = r.applyRedactors("trace.scenario_name", out.ScenarioName)
	out.Metadata = redactMetadata(r.redactors, "trace.metadata", trace.Metadata)
	if len(trace.Spans) > 0 {
		out.Spans = make([]Span, len(trace.Spans))
		copy(out.Spans, trace.Spans)
		for i := range out.Spans {
			path := "trace.spans." + strconv.Itoa(i)
			out.Spans[i].Name = r.applyRedactors(path+".name", out.Spans[i].Name)
			out.Spans[i].Input = r.applyRedactors(path+".input", out.Spans[i].Input)
			out.Spans[i].Output = r.applyRedactors(path+".output", out.Spans[i].Output)
			out.Spans[i].Error = r.applyRedactors(path+".error", out.Spans[i].Error)
			out.Spans[i].Metadata = redactMetadata(r.redactors, path+".metadata", trace.Spans[i].Metadata)
			if trace.Spans[i].ToolCall != nil {
				call := *trace.Spans[i].ToolCall
				call.Name = r.applyRedactors(path+".tool_call.name", call.Name)
				call.Result = r.applyRedactors(path+".tool_call.result", call.Result)
				call.Error = r.applyRedactors(path+".tool_call.error", call.Error)
				call.Arguments = redactRawJSON(r.redactors, path+".tool_call.arguments", call.Arguments)
				call.Metadata = redactMetadata(r.redactors, path+".tool_call.metadata", call.Metadata)
				out.Spans[i].ToolCall = &call
			}
		}
	}
	if len(trace.Artifacts) > 0 {
		out.Artifacts = make([]ArtifactRecord, len(trace.Artifacts))
		copy(out.Artifacts, trace.Artifacts)
		for i := range out.Artifacts {
			path := "trace.artifacts." + strconv.Itoa(i)
			out.Artifacts[i].Key = r.applyRedactors(path+".key", out.Artifacts[i].Key)
			out.Artifacts[i].Name = r.applyRedactors(path+".name", out.Artifacts[i].Name)
			out.Artifacts[i].URI = r.applyRedactors(path+".uri", out.Artifacts[i].URI)
			out.Artifacts[i].Value = redactRawJSON(r.redactors, path+".value", out.Artifacts[i].Value)
			out.Artifacts[i].Metadata = redactMetadata(r.redactors, path+".metadata", trace.Artifacts[i].Metadata)
		}
	}
	if len(trace.StateDeltas) > 0 {
		out.StateDeltas = make([]StateDelta, len(trace.StateDeltas))
		copy(out.StateDeltas, trace.StateDeltas)
		for i := range out.StateDeltas {
			path := "trace.state_deltas." + strconv.Itoa(i)
			out.StateDeltas[i].Key = r.applyRedactors(path+".key", out.StateDeltas[i].Key)
			out.StateDeltas[i].Before = redactRawJSON(r.redactors, path+".before", out.StateDeltas[i].Before)
			out.StateDeltas[i].After = redactRawJSON(r.redactors, path+".after", out.StateDeltas[i].After)
			out.StateDeltas[i].Metadata = redactMetadata(r.redactors, path+".metadata", trace.StateDeltas[i].Metadata)
		}
	}
	return out
}

func redactMetadata(redactors []Redactor, path string, metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}

	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = redactAny(redactors, path+"."+key, value)
	}
	return out
}

func redactAny(redactors []Redactor, path string, value any) any {
	switch v := value.(type) {
	case string:
		return redactString(redactors, path, v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[key] = redactAny(redactors, path+"."+key, nested)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, nested := range v {
			out[key] = redactString(redactors, path+"."+key, nested)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, nested := range v {
			out[i] = redactAny(redactors, path+"."+strconv.Itoa(i), nested)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, nested := range v {
			redacted := make(map[string]any, len(nested))
			for key, item := range nested {
				redacted[key] = redactAny(redactors, path+"."+strconv.Itoa(i)+"."+key, item)
			}
			out[i] = redacted
		}
		return out
	case []map[string]string:
		out := make([]map[string]string, len(v))
		for i, nested := range v {
			redacted := make(map[string]string, len(nested))
			for key, item := range nested {
				redacted[key] = redactString(redactors, path+"."+strconv.Itoa(i)+"."+key, item)
			}
			out[i] = redacted
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, nested := range v {
			out[i] = redactString(redactors, path+"."+strconv.Itoa(i), nested)
		}
		return out
	default:
		return v
	}
}

func redactString(redactors []Redactor, path string, value string) string {
	for _, redactor := range redactors {
		value = redactor(path, value)
	}
	return value
}

func redactRawJSON(redactors []Redactor, path string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return cloneRawMessage(raw)
	}
	redacted := redactAny(redactors, path, value)
	out, err := json.Marshal(redacted)
	if err != nil {
		return cloneRawMessage(raw)
	}
	return out
}

func redactScenarioSummary(redactors []Redactor, summary *ScenarioSummary) *ScenarioSummary {
	if summary == nil {
		return nil
	}
	out := *summary
	out.Metadata = redactMetadata(redactors, "scenario_summary.metadata", summary.Metadata)
	if len(summary.Dimensions) > 0 {
		out.Dimensions = make([]DimensionResult, len(summary.Dimensions))
		copy(out.Dimensions, summary.Dimensions)
		for i := range out.Dimensions {
			path := "scenario_summary.dimensions." + strconv.Itoa(i) + ".reason"
			out.Dimensions[i].Reason = redactString(redactors, path, out.Dimensions[i].Reason)
		}
	}
	if len(summary.Steps) > 0 {
		out.Steps = make([]StepSummary, len(summary.Steps))
		copy(out.Steps, summary.Steps)
		for i := range out.Steps {
			stepPath := "scenario_summary.steps." + strconv.Itoa(i)
			out.Steps[i].Metadata = redactMetadata(redactors, stepPath+".metadata", summary.Steps[i].Metadata)
			out.Steps[i].FailedMetrics = redactStringSlice(redactors, stepPath+".failed_metrics", summary.Steps[i].FailedMetrics)
			out.Steps[i].ToolCalls = redactStringSlice(redactors, stepPath+".tool_calls", summary.Steps[i].ToolCalls)
			out.Steps[i].ArtifactKeys = redactStringSlice(redactors, stepPath+".artifact_keys", summary.Steps[i].ArtifactKeys)
		}
	}
	out.ArtifactKeys = redactStringSlice(redactors, "scenario_summary.artifact_keys", summary.ArtifactKeys)
	return &out
}

func redactStringSlice(redactors []Redactor, path string, values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = redactString(redactors, path+"."+strconv.Itoa(i), value)
	}
	return out
}

func finalPathSegment(path string) string {
	if path == "" {
		return ""
	}
	i := strings.LastIndex(path, ".")
	if i == -1 {
		return path
	}
	return path[i+1:]
}

func isMetadataPath(path string) bool {
	return strings.HasPrefix(path, "metadata.") || strings.Contains(path, ".metadata.")
}
