package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestArtifactExists_Found(t *testing.T) {
	r, err := (ArtifactExists{Key: "route"}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"status":"ready"}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactExists_Missing(t *testing.T) {
	r, err := (ArtifactExists{Key: "route"}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(r.Reason, `artifact "route" not found`) {
		t.Fatalf("reason missing artifact key: %q", r.Reason)
	}
}

func TestArtifactMetrics_NilArtifactsFailGracefully(t *testing.T) {
	tests := []struct {
		name   string
		metric Metric
	}{
		{
			name:   "json path",
			metric: ArtifactJSONPath{Key: "route", Path: "status", Expected: "ready"},
		},
		{
			name:   "field count",
			metric: ArtifactFieldCount{Key: "route", MinFields: 1},
		},
		{
			name:   "number lte",
			metric: ArtifactNumberLTE{Key: "route", Path: "total_minutes", Max: 120},
		},
		{
			name:   "array contains",
			metric: ArtifactArrayContains{Key: "route", Path: "stops", Expected: "Pajaritos"},
		},
		{
			name:   "array not contains",
			metric: ArtifactArrayNotContains{Key: "route", Path: "stops", Expected: "Pajaritos"},
		},
		{
			name:   "subset",
			metric: ArtifactSubset{Key: "route", Expected: json.RawMessage(`{"status":"ready"}`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := tt.metric.Score(context.Background(), nil, Case{})
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if r.Passed || r.Score != 0 {
				t.Fatalf("expected graceful failure, got %+v", r)
			}
			if !strings.Contains(r.Reason, `artifact "route" not found`) {
				t.Fatalf("reason missing artifact key: %q", r.Reason)
			}
		})
	}
}

func TestArtifactJSONPath_Match(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "route",
		Path:     "legs[0].mode",
		Expected: "metro",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"legs":[{"mode":"metro"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactJSONPath_BoolExpectedUsesJSONLiteralString(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "state",
		Path:     "payment.ready",
		Expected: "true",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"state": json.RawMessage(`{"payment":{"ready":true}}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactJSONPath_EmptyPathComparesRoot(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "status",
		Expected: "ready",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"status": json.RawMessage(`"ready"`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactJSONPath_Mismatch(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "route",
		Path:     "status",
		Expected: "ready",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"status":"blocked"}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(r.Reason, `got "blocked", expected "ready"`) {
		t.Fatalf("unexpected reason: %q", r.Reason)
	}
}

func TestArtifactFieldCount(t *testing.T) {
	r, err := (ArtifactFieldCount{
		Key:       "state",
		Path:      "payment",
		MinFields: 2,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"state": json.RawMessage(`{"payment":{"method":"card","authorized":true,"receipt":null}}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactFieldCount_TooFew(t *testing.T) {
	r, err := (ArtifactFieldCount{
		Key:       "state",
		MinFields: 3,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"state": json.RawMessage(`{"a":1,"b":null}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed {
		t.Fatalf("unexpected pass: %+v", r)
	}
	if r.Score != 1.0/3.0 {
		t.Fatalf("score mismatch: got %.6f", r.Score)
	}
}

func TestArtifactFieldCount_InvalidMinimum(t *testing.T) {
	r, err := (ArtifactFieldCount{
		Key:       "state",
		MinFields: 0,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"state": json.RawMessage(`{"a":1}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(r.Reason, "MinFields must be >= 1") {
		t.Fatalf("unexpected reason: %q", r.Reason)
	}
}

func TestArtifactNumberLTE(t *testing.T) {
	r, err := (ArtifactNumberLTE{
		Key:  "budget",
		Path: "tokens",
		Max:  800,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"budget": json.RawMessage(`{"tokens":742}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactNumberLTE_TooHigh(t *testing.T) {
	r, err := (ArtifactNumberLTE{
		Key:  "budget",
		Path: "tokens",
		Max:  800,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"budget": json.RawMessage(`{"tokens":801}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactArrayContains(t *testing.T) {
	r, err := (ArtifactArrayContains{
		Key:      "route",
		Path:     "stops",
		Expected: "Pajaritos",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"stops":["Universidad de Santiago","Pajaritos","Valparaiso"]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactArrayContains_Missing(t *testing.T) {
	r, err := (ArtifactArrayContains{
		Key:      "route",
		Path:     "stops",
		Expected: "Airport",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"stops":["Universidad de Santiago","Pajaritos"]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactArrayMinLen(t *testing.T) {
	r, err := (ArtifactArrayMinLen{
		Key:    "route",
		Path:   "stops",
		MinLen: 2,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"stops":["Universidad de Santiago","Pajaritos"]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactArrayMinLen_TooShort(t *testing.T) {
	r, err := (ArtifactArrayMinLen{
		Key:    "route",
		Path:   "stops",
		MinLen: 3,
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"stops":["Pajaritos"]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || r.Score != 1.0/3.0 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactArrayMinLen_FailsGracefully(t *testing.T) {
	tests := []struct {
		name   string
		metric ArtifactArrayMinLen
		c      Case
		want   string
	}{
		{
			name:   "missing path",
			metric: ArtifactArrayMinLen{Key: "route", Path: "stops", MinLen: 1},
			c: Case{Artifacts: map[string]json.RawMessage{
				"route": json.RawMessage(`{"status":"ready"}`),
			}},
			want: `key "stops" not found`,
		},
		{
			name:   "non array",
			metric: ArtifactArrayMinLen{Key: "route", Path: "status", MinLen: 1},
			c: Case{Artifacts: map[string]json.RawMessage{
				"route": json.RawMessage(`{"status":"ready"}`),
			}},
			want: "is not a JSON array",
		},
		{
			name:   "invalid minimum",
			metric: ArtifactArrayMinLen{Key: "route", Path: "stops"},
			c: Case{Artifacts: map[string]json.RawMessage{
				"route": json.RawMessage(`{"stops":[]}`),
			}},
			want: "MinLen must be >= 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := tt.metric.Score(context.Background(), nil, tt.c)
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if r.Passed || !strings.Contains(r.Reason, tt.want) {
				t.Fatalf("unexpected result: %+v", r)
			}
		})
	}
}

func TestArtifactMetric_InvalidJSON(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "route",
		Path:     "status",
		Expected: "ready",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"status":`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || !strings.Contains(r.Reason, "not valid JSON") {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactMetric_InvalidPath(t *testing.T) {
	r, err := (ArtifactJSONPath{
		Key:      "route",
		Path:     "legs[*].mode",
		Expected: "metro",
	}).Score(context.Background(), nil, Case{
		Artifacts: map[string]json.RawMessage{
			"route": json.RawMessage(`{"legs":[{"mode":"metro"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if r.Passed || !strings.Contains(r.Reason, "unsupported JSONPath") {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestArtifactNotExists(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"status":"ready"}`),
	}}

	pass, err := (ArtifactNotExists{Key: "budget"}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !pass.Passed {
		t.Fatalf("expected missing artifact to pass, got %+v", pass)
	}

	fail, err := (ArtifactNotExists{Key: "route"}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if fail.Passed {
		t.Fatalf("expected existing artifact to fail, got %+v", fail)
	}

	nilArtifactsPass, err := (ArtifactNotExists{Key: "route"}).Score(context.Background(), nil, Case{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !nilArtifactsPass.Passed {
		t.Fatalf("expected missing artifact in nil map to pass, got %+v", nilArtifactsPass)
	}
}

func TestArtifactArrayNotContainsWithNormalizer(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"stops":["USACH","Pajaritos"]}`),
	}}

	result, err := (ArtifactArrayNotContains{
		Key:        "route",
		Path:       "stops",
		Expected:   "los heroes",
		Normalizer: ChainNormalizers(CaseFoldNormalizer(), SpanishASCIIFoldNormalizer()),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected absent value to pass, got %+v", result)
	}

	result, err = (ArtifactArrayNotContains{
		Key:        "route",
		Path:       "stops",
		Expected:   "pájaritos",
		Normalizer: ChainNormalizers(CaseFoldNormalizer(), SpanishASCIIFoldNormalizer()),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected normalized present value to fail, got %+v", result)
	}
}

func TestArtifactArrayContainsWildcardPath(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"stops":[{"name":"USACH"},{"name":"Pajaritos"}]}`),
	}}

	result, err := (ArtifactArrayContains{
		Key:      "route",
		Path:     "stops[*].name",
		Expected: "Pajaritos",
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected wildcard path to match, got %+v", result)
	}
}

func TestArtifactWildcardPathErrorsOnNonArray(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"stops":{"name":"USACH"}}`),
	}}

	result, err := (ArtifactArrayContains{
		Key:      "route",
		Path:     "stops[*].name",
		Expected: "USACH",
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "current node is not an array") {
		t.Fatalf("expected wildcard non-array failure, got %+v", result)
	}
}

func TestArtifactSubsetMatchesPartialJSON(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"success":true,"routeStatus":"ready","stops":[{"name":"Pajaritos","id":2},{"name":"USACH","id":1}]}`),
	}}

	result, err := (ArtifactSubset{
		Key: "route",
		Expected: json.RawMessage(`{
			"success": true,
			"routeStatus": "READY",
			"stops": [{"name": "usách"}]
		}`),
		Normalizer: ChainNormalizers(CaseFoldNormalizer(), SpanishASCIIFoldNormalizer()),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected subset match, got %+v", result)
	}
}

func TestArtifactSubsetWildcardPath(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"stops":[{"name":"USACH","id":1},{"name":"Pajaritos","id":2}]}`),
	}}

	result, err := (ArtifactSubset{
		Key:        "route",
		Path:       "stops[*]",
		Expected:   json.RawMessage(`{"name":"pájaritos"}`),
		Normalizer: ChainNormalizers(CaseFoldNormalizer(), SpanishASCIIFoldNormalizer()),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected wildcard subset match, got %+v", result)
	}
}

func TestArtifactSubsetArrayMatcherBacktracks(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"stops":[{"name":"USACH","id":1},{"name":"USACH","id":2}]}`),
	}}

	result, err := (ArtifactSubset{
		Key:      "route",
		Expected: json.RawMessage(`{"stops":[{"name":"USACH"},{"id":1}]}`),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected array subset matcher to find a non-greedy assignment, got %+v", result)
	}
}

func TestArtifactSubsetArrayMatcherMemoizedBoundary(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"items": artifactObjectArray(64),
	}}

	result, err := (ArtifactSubset{
		Key:      "items",
		Expected: json.RawMessage(`[{"id":0},{"id":63}]`),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected 64-element boundary match, got %+v", result)
	}
}

func TestArtifactSubsetArrayMatcherFallbackPastMemoizedBoundary(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"items": artifactObjectArray(65),
	}}

	result, err := (ArtifactSubset{
		Key:      "items",
		Expected: json.RawMessage(`[{"id":64}]`),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected >64-element fallback match, got %+v", result)
	}
}

func TestArtifactSubsetReportsMismatch(t *testing.T) {
	c := Case{Artifacts: map[string]json.RawMessage{
		"route": json.RawMessage(`{"success":true}`),
	}}

	result, err := (ArtifactSubset{
		Key:      "route",
		Expected: json.RawMessage(`{"routeStatus":"ready"}`),
	}).Score(context.Background(), nil, c)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if result.Passed || !strings.Contains(result.Reason, "routeStatus is missing") {
		t.Fatalf("expected subset mismatch, got %+v", result)
	}
}

func artifactObjectArray(n int) json.RawMessage {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d,"type":"x"}`, i)
	}
	b.WriteByte(']')
	return json.RawMessage(b.String())
}
