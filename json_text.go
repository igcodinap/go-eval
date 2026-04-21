package eval

import (
	"encoding/json"
	"strings"
)

// StripMarkdownCodeFence removes a surrounding markdown code fence when present.
func StripMarkdownCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	lines = lines[1:]
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" {
			lines = lines[:len(lines)-1]
			continue
		}
		if strings.HasPrefix(last, "```") {
			lines = lines[:len(lines)-1]
		}
		break
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ExtractJSONObjectCandidate returns a best-effort JSON object candidate string.
//
// It first strips markdown fences, then:
//   - returns the full payload if it is already valid JSON
//   - scans for the first decodable JSON object in mixed prose
func ExtractJSONObjectCandidate(s string) string {
	clean := StripMarkdownCodeFence(s)
	if json.Valid([]byte(clean)) {
		return clean
	}

	for i := 0; i < len(clean); i++ {
		if clean[i] != '{' {
			continue
		}

		dec := json.NewDecoder(strings.NewReader(clean[i:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			continue
		}

		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		return strings.TrimSpace(string(raw))
	}

	return clean
}
