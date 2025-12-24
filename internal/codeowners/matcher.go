package codeowners

import (
	"bufio"
	"path/filepath"
	"strings"
)

// rule represents a single CODEOWNERS rule
type rule struct {
	pattern string
	owners  []string
}

// Matcher matches files against CODEOWNERS rules
type Matcher struct {
	rules []rule
}

// ParseCodeowners parses CODEOWNERS content and returns a Matcher
func ParseCodeowners(content string) *Matcher {
	var rules []rule
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		pattern := parts[0]
		owners := parts[1:]

		rules = append(rules, rule{
			pattern: pattern,
			owners:  owners,
		})
	}

	return &Matcher{rules: rules}
}

// Match returns the owners for a given file path
// Later rules take precedence (as per CODEOWNERS spec)
func (m *Matcher) Match(filePath string) []string {
	var matchedOwners []string

	for _, r := range m.rules {
		if matchPattern(r.pattern, filePath) {
			matchedOwners = r.owners
		}
	}

	return matchedOwners
}

// matchPattern checks if a file path matches a CODEOWNERS pattern
func matchPattern(pattern, filePath string) bool {
	// Remove leading slash (relative to repo root)
	pattern = strings.TrimPrefix(pattern, "/")

	// Handle directory patterns (ending with /)
	if strings.HasSuffix(pattern, "/") {
		dirPattern := strings.TrimSuffix(pattern, "/")
		// Match if file is in this directory or subdirectory
		if strings.HasPrefix(filePath, dirPattern+"/") {
			return true
		}
		return filePath == dirPattern
	}

	// Handle ** (matches any path)
	if strings.Contains(pattern, "**") {
		return matchDoubleGlob(pattern, filePath)
	}

	// Handle * (matches within single path component)
	if strings.Contains(pattern, "*") {
		matched, _ := filepath.Match(pattern, filePath)
		if matched {
			return true
		}
		// Also try matching just the filename
		matched, _ = filepath.Match(pattern, filepath.Base(filePath))
		if matched {
			return true
		}
		// Try matching with directory prefix
		return matchGlobInPath(pattern, filePath)
	}

	// Exact match
	if pattern == filePath {
		return true
	}

	// Pattern matches directory prefix
	if strings.HasPrefix(filePath, pattern+"/") {
		return true
	}

	// Pattern matches filename
	if filepath.Base(filePath) == pattern {
		return true
	}

	return false
}

// matchDoubleGlob handles ** patterns
func matchDoubleGlob(pattern, filePath string) bool {
	// Split pattern by **
	parts := strings.Split(pattern, "**")

	if len(parts) == 2 {
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")

		// Check prefix
		if prefix != "" && !strings.HasPrefix(filePath, prefix) {
			// Also check if prefix matches with trailing slash
			if !strings.HasPrefix(filePath, prefix+"/") {
				return false
			}
		}

		// Check suffix
		if suffix != "" {
			if strings.Contains(suffix, "*") {
				// Handle glob in suffix
				matched, _ := filepath.Match(suffix, filepath.Base(filePath))
				return matched
			}
			return strings.HasSuffix(filePath, suffix) || strings.HasSuffix(filePath, "/"+suffix)
		}

		return true
	}

	return false
}

// matchGlobInPath tries to match a glob pattern against path components
func matchGlobInPath(pattern, filePath string) bool {
	// Try matching pattern against each possible subpath
	parts := strings.Split(filePath, "/")
	patternParts := strings.Split(pattern, "/")

	if len(patternParts) > len(parts) {
		return false
	}

	// Try matching from each starting position
	for i := 0; i <= len(parts)-len(patternParts); i++ {
		match := true
		for j, pp := range patternParts {
			matched, _ := filepath.Match(pp, parts[i+j])
			if !matched {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}
