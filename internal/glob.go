package internal

import (
	"regexp"
	"strings"
)

// globToRegex translates a glob pattern (**, *, ?) into a regexp that matches
// a POSIX-style relative path.
//
//   - *   matches within a single path segment (no /)
//   - **  matches across segment boundaries (any depth)
//   - ?   matches exactly one non-separator character
func globToRegex(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?s:")
	i := 0
	for i < len(pattern) {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			// double-star
			if i+2 < len(pattern) && pattern[i+2] == '/' {
				// "**/" — optional leading directory segments
				b.WriteString(`(?:.*/)?`)
				i += 3
			} else {
				b.WriteString(`.*`)
				i += 2
			}
		case c == '*':
			b.WriteString(`[^/]*`)
			i++
		case c == '?':
			b.WriteString(`[^/]`)
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	b.WriteString(`)\z`)
	return regexp.MustCompile(b.String())
}

// matchesAny reports whether rel matches at least one glob pattern.
func matchesAny(rel string, patterns []string) bool {
	for _, p := range patterns {
		if globToRegex(p).MatchString(rel) {
			return true
		}
	}
	return false
}
