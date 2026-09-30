package query

import (
	"strings"
	"unicode"
)

// Matcher performs case-insensitive matching of search terms against text.
// Every term must be present for a match (logical AND). Double-quoted
// sections are treated as a single phrase term.
type Matcher struct {
	terms []string
}

// NewMatcher parses q into a Matcher. An empty query matches everything.
func NewMatcher(q string) Matcher {
	var terms []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if t := normalize(cur.String()); t != "" {
			terms = append(terms, t)
		}
		cur.Reset()
	}
	for _, r := range q {
		switch {
		case r == '"':
			flush()
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return Matcher{terms: terms}
}

// Terms returns the normalized terms used for matching.
func (m Matcher) Terms() []string { return m.terms }

// Match reports whether all terms occur in the given texts combined.
func (m Matcher) Match(texts ...string) bool {
	if len(m.terms) == 0 {
		return true
	}
	haystack := normalize(strings.Join(texts, "\n"))
	for _, t := range m.terms {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

// normalize lower-cases s and collapses all runs of whitespace to one space.
func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
