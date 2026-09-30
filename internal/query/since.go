// Package query contains helpers for filtering Teams messages by time and text.
package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var relativeRe = regexp.MustCompile(`^(\d+)\s*(m|min|mins|h|hr|hrs|d|day|days|w|wk|wks|week|weeks|mo|month|months|y|yr|yrs|year|years)$`)

// ParseSince converts a user-supplied "since" value into an absolute time.
//
// Supported forms:
//   - relative durations: 90m, 12h, 30d, 2w, 3mo, 1y
//   - dates: 2024-05-01 (interpreted as UTC midnight)
//   - RFC 3339 timestamps: 2024-05-01T09:00:00Z
//
// An empty string returns the zero time (no lower bound).
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if m := relativeRe.FindStringSubmatch(strings.ToLower(s)); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid since value %q: %w", s, err)
		}
		switch m[2] {
		case "m", "min", "mins":
			return now.Add(-time.Duration(n) * time.Minute), nil
		case "h", "hr", "hrs":
			return now.Add(-time.Duration(n) * time.Hour), nil
		case "d", "day", "days":
			return now.AddDate(0, 0, -n), nil
		case "w", "wk", "wks", "week", "weeks":
			return now.AddDate(0, 0, -7*n), nil
		case "mo", "month", "months":
			return now.AddDate(0, -n, 0), nil
		case "y", "yr", "yrs", "year", "years":
			return now.AddDate(-n, 0, 0), nil
		}
	}
	if t, err := time.Parse(time.RFC3339, strings.ToUpper(s)); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid since value %q: use a relative duration (e.g. 30d, 12h, 2w, 3mo), a date (YYYY-MM-DD) or an RFC 3339 timestamp", s)
}
