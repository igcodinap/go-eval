package eval

import (
	"encoding/json"
	"regexp"
	"strings"
)

var jsonObjectRE = regexp.MustCompile(`(?s)\{.*\}`)

func stripMarkdownCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return trimmed
	}

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

func extractJSONObjectCandidate(s string) string {
	clean := stripMarkdownCodeFence(s)
	if json.Valid([]byte(clean)) {
		return clean
	}

	match := jsonObjectRE.FindString(clean)
	if match != "" {
		return strings.TrimSpace(match)
	}

	return clean
}
