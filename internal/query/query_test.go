package query

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"":                     {},
		"30d":                  now.AddDate(0, 0, -30),
		"30 days":              now.AddDate(0, 0, -30),
		"12h":                  now.Add(-12 * time.Hour),
		"90m":                  now.Add(-90 * time.Minute),
		"2w":                   now.AddDate(0, 0, -14),
		"3mo":                  now.AddDate(0, -3, 0),
		"1y":                   now.AddDate(-1, 0, 0),
		"2026-09-01":           time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		"2026-09-01T10:30:00Z": time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseSince(in, now)
		if err != nil {
			t.Errorf("ParseSince(%q) error: %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"yesterday", "30x", "-5d", "2026-13-01"} {
		if _, err := ParseSince(bad, now); err == nil {
			t.Errorf("ParseSince(%q) expected error", bad)
		}
	}
}

func TestMatcher(t *testing.T) {
	cases := []struct {
		query, text string
		want        bool
	}{
		{"private endpoints", "We need Private   Endpoints for the storage account", true},
		{"private endpoints", "the endpoint is private", false},
		{"private endpoint", "the endpoint is private", true},
		{`"private endpoint"`, "the endpoint is private", false},
		{`"private endpoint" dns`, "Private\nEndpoint DNS zones", true},
		{"", "anything", true},
		{"kubernetes", "AKS cluster", false},
	}
	for _, c := range cases {
		if got := NewMatcher(c.query).Match(c.text); got != c.want {
			t.Errorf("NewMatcher(%q).Match(%q) = %v, want %v", c.query, c.text, got, c.want)
		}
	}
	if got := NewMatcher(`"Private  Endpoint" DNS`).Terms(); len(got) != 2 || got[0] != "private endpoint" || got[1] != "dns" {
		t.Errorf("unexpected terms %q", got)
	}
}
