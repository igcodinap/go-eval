package eval

import (
	"fmt"
	pathpkg "path"
)

func toolNameMatchesAnyPattern(name string, patterns []string) (bool, string, error) {
	for _, pattern := range patterns {
		matched, err := toolNameMatchesPattern(name, pattern)
		if err != nil {
			return false, "", err
		}
		if matched {
			return true, pattern, nil
		}
	}
	return false, "", nil
}

func toolNameMatchesPattern(name string, pattern string) (bool, error) {
	matched, err := pathpkg.Match(pattern, name)
	if err != nil {
		return false, fmt.Errorf("invalid tool pattern %q: %v", pattern, err)
	}
	return matched, nil
}

func toolNameIn(name string, names []string) bool {
	for _, item := range names {
		if name == item {
			return true
		}
	}
	return false
}

func validateToolPatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := pathpkg.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid tool pattern %q: %w", pattern, err)
		}
	}
	return nil
}
