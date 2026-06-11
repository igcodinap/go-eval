package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkArtifactSubsetBacktrackingAdversarial(b *testing.B) {
	for _, n := range []int{4, 6, 8} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			c := Case{
				Artifacts: map[string]json.RawMessage{
					"items": artifactObjectArray(n),
				},
			}
			metric := ArtifactSubset{
				Key:      "items",
				Expected: adversarialArtifactSubset(n),
			}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := metric.Score(ctx, nil, c)
				if err != nil {
					b.Fatalf("Score: %v", err)
				}
				if result.Passed {
					b.Fatalf("expected adversarial subset to fail")
				}
			}
		})
	}
}

func BenchmarkArtifactArrayContainsMissingValue(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			c := Case{
				Artifacts: map[string]json.RawMessage{
					"items": artifactObjectArray(n),
				},
			}
			metric := ArtifactArrayContains{
				Key:      "items",
				Expected: `{"id":-1,"type":"x"}`,
			}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := metric.Score(ctx, nil, c)
				if err != nil {
					b.Fatalf("Score: %v", err)
				}
				if result.Passed {
					b.Fatalf("expected missing array value to fail")
				}
			}
		})
	}
}

func adversarialArtifactSubset(n int) json.RawMessage {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n-1; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"type":"x"}`)
	}
	if n > 1 {
		b.WriteByte(',')
	}
	b.WriteString(`{"id":-1}`)
	b.WriteByte(']')
	return json.RawMessage(b.String())
}
