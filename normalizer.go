package eval

import "strings"

// Normalizer rewrites text before deterministic string comparison.
type Normalizer func(string) string

// CaseFoldNormalizer returns a normalizer that compares strings case-insensitively.
func CaseFoldNormalizer() Normalizer {
	return strings.ToLower
}

// SpanishASCIIFoldNormalizer folds common Spanish accented characters to ASCII.
func SpanishASCIIFoldNormalizer() Normalizer {
	replacer := strings.NewReplacer(
		"á", "a", "Á", "A",
		"é", "e", "É", "E",
		"í", "i", "Í", "I",
		"ó", "o", "Ó", "O",
		"ú", "u", "Ú", "U",
		"ü", "u", "Ü", "U",
		"ñ", "n", "Ñ", "N",
	)
	return replacer.Replace
}

// ChainNormalizers returns a normalizer that applies each normalizer in order.
func ChainNormalizers(normalizers ...Normalizer) Normalizer {
	return func(s string) string {
		for _, normalizer := range normalizers {
			if normalizer != nil {
				s = normalizer(s)
			}
		}
		return s
	}
}

func normalizeString(normalizer Normalizer, s string) string {
	if normalizer == nil {
		return s
	}
	return normalizer(s)
}
