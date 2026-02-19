package tool

import (
	"path/filepath"
	"regexp"
	"strings"
)

func matchesGlobPattern(pattern string, value string) bool {
	trimmedPattern := strings.TrimSpace(pattern)
	if trimmedPattern == "" {
		return false
	}

	normalizedValue := filepath.ToSlash(strings.TrimSpace(value))
	if normalizedValue == "" {
		return false
	}

	matcher, err := compileGlobPattern(trimmedPattern)
	if err != nil {
		return false
	}
	if matcher.MatchString(normalizedValue) {
		return true
	}

	normalizedPattern := filepath.ToSlash(trimmedPattern)
	if !strings.Contains(normalizedPattern, "/") {
		baseMatcher, err := compileGlobPattern(normalizedPattern)
		if err != nil {
			return false
		}
		return baseMatcher.MatchString(filepath.Base(normalizedValue))
	}
	return false
}

func compileGlobPattern(pattern string) (*regexp.Regexp, error) {
	normalized := filepath.ToSlash(strings.TrimSpace(pattern))
	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(normalized); i++ {
		ch := normalized[i]
		switch ch {
		case '*':
			if i+1 < len(normalized) && normalized[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			if strings.ContainsRune(`.+()|[]{}^$\`, rune(ch)) {
				b.WriteByte('\\')
			}
			b.WriteByte(ch)
		}
	}

	b.WriteString("$")
	return regexp.Compile(b.String())
}
