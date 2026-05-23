package eval

import (
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
		if field == "" || !strings.HasPrefix(path, "metadata.") || finalPathSegment(path) != field {
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
	return out
}

func (r *Runner) applyRedactors(path string, value string) string {
	for _, redactor := range r.redactors {
		value = redactor(path, value)
	}
	return value
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
