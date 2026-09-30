// Package teams implements the Teams-level operations used by the CLI:
// resolving teams and channels by name, and collecting/searching messages.
package teams

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
)

// Service wraps a Graph client with Teams-specific helpers.
type Service struct {
	Graph *graph.Client
}

// ChannelRef identifies a channel together with its parent team.
type ChannelRef struct {
	TeamID         string `json:"teamId"`
	TeamName       string `json:"teamName"`
	ChannelID      string `json:"channelId"`
	ChannelName    string `json:"channelName"`
	MembershipType string `json:"membershipType,omitempty"`
	WebURL         string `json:"webUrl,omitempty"`
}

// NotFoundError is returned when a team or channel cannot be resolved.
type NotFoundError struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Available []string `json:"available,omitempty"`
}

func (e *NotFoundError) Error() string {
	msg := fmt.Sprintf("%s %q not found", e.Kind, e.Name)
	if len(e.Available) > 0 {
		msg += "; available: " + strings.Join(e.Available, ", ")
	}
	return msg
}

// AmbiguousError is returned when a name matches more than one channel/team.
type AmbiguousError struct {
	Kind    string       `json:"kind"`
	Name    string       `json:"name"`
	Matches []ChannelRef `json:"matches"`
}

func (e *AmbiguousError) Error() string {
	var names []string
	for _, m := range e.Matches {
		if e.Kind == "team" {
			names = append(names, fmt.Sprintf("%s (%s)", m.TeamName, m.TeamID))
		} else {
			names = append(names, fmt.Sprintf("%s/%s", m.TeamName, m.ChannelName))
		}
	}
	return fmt.Sprintf("%s %q is ambiguous; use --team to disambiguate. Matches: %s", e.Kind, e.Name, strings.Join(names, ", "))
}

var guidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// nameScore rates how well a user-supplied name matches a display name:
// 2 = case-insensitive exact, 1 = equal ignoring punctuation/spacing
// (so "platform-engineering" matches "Platform Engineering"), 0 = no match.
func nameScore(query, name string) int {
	if strings.EqualFold(strings.TrimSpace(query), strings.TrimSpace(name)) {
		return 2
	}
	if q := squash(query); q != "" && q == squash(name) {
		return 1
	}
	return 0
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ResolveTeams returns the joined teams matching teamArg (ID or display
// name). An empty teamArg returns all joined teams.
func (s *Service) ResolveTeams(ctx context.Context, teamArg string) ([]graph.Team, error) {
	joined, err := s.Graph.JoinedTeams(ctx)
	if err != nil {
		return nil, err
	}
	if teamArg == "" {
		return joined, nil
	}
	for _, t := range joined {
		if strings.EqualFold(t.ID, teamArg) {
			return []graph.Team{t}, nil
		}
	}
	best, matches := 0, []graph.Team(nil)
	for _, t := range joined {
		sc := nameScore(teamArg, t.DisplayName)
		switch {
		case sc == 0:
		case sc > best:
			best, matches = sc, []graph.Team{t}
		case sc == best:
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches, nil
	case 0:
		if guidRe.MatchString(teamArg) {
			t, err := s.Graph.Team(ctx, teamArg)
			if err == nil {
				return []graph.Team{t}, nil
			}
		}
		names := make([]string, 0, len(joined))
		for _, t := range joined {
			names = append(names, t.DisplayName)
		}
		return nil, &NotFoundError{Kind: "team", Name: teamArg, Available: capNames(names)}
	default:
		amb := &AmbiguousError{Kind: "team", Name: teamArg}
		for _, t := range matches {
			amb.Matches = append(amb.Matches, ChannelRef{TeamID: t.ID, TeamName: t.DisplayName})
		}
		return nil, amb
	}
}

// ListChannels returns all channels in the teams matching teamArg.
func (s *Service) ListChannels(ctx context.Context, teamArg string) ([]ChannelRef, error) {
	teams, err := s.ResolveTeams(ctx, teamArg)
	if err != nil {
		return nil, err
	}
	var out []ChannelRef
	for _, t := range teams {
		chans, err := s.Graph.Channels(ctx, t.ID)
		if err != nil {
			// Skip teams whose channels the user cannot read when listing
			// across all teams; surface the error for an explicit team.
			if teamArg == "" && (graph.IsStatus(err, 403) || graph.IsStatus(err, 404)) {
				continue
			}
			return nil, err
		}
		for _, c := range chans {
			out = append(out, ChannelRef{
				TeamID: t.ID, TeamName: t.DisplayName,
				ChannelID: c.ID, ChannelName: c.DisplayName,
				MembershipType: c.MembershipType, WebURL: c.WebURL,
			})
		}
	}
	return out, nil
}

// ResolveChannels resolves the channels to operate on. channelArg may be a
// channel ID (19:...) or display name; if empty, all channels of the team(s)
// are returned. At least one of teamArg or channelArg must be set.
func (s *Service) ResolveChannels(ctx context.Context, teamArg, channelArg string) ([]ChannelRef, error) {
	if teamArg == "" && channelArg == "" {
		return nil, fmt.Errorf("at least one of --channel or --team is required")
	}
	all, err := s.ListChannels(ctx, teamArg)
	if err != nil {
		return nil, err
	}
	if channelArg == "" {
		return all, nil
	}
	for _, c := range all {
		if c.ChannelID == channelArg {
			return []ChannelRef{c}, nil
		}
	}
	best, matches := 0, []ChannelRef(nil)
	for _, c := range all {
		sc := nameScore(channelArg, c.ChannelName)
		switch {
		case sc == 0:
		case sc > best:
			best, matches = sc, []ChannelRef{c}
		case sc == best:
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 1:
		return matches, nil
	case 0:
		seen := map[string]bool{}
		var names []string
		for _, c := range all {
			if !seen[c.ChannelName] {
				seen[c.ChannelName] = true
				names = append(names, c.ChannelName)
			}
		}
		return nil, &NotFoundError{Kind: "channel", Name: channelArg, Available: capNames(names)}
	default:
		return nil, &AmbiguousError{Kind: "channel", Name: channelArg, Matches: matches}
	}
}

// maxAvailableNames bounds the suggestions included in a NotFoundError.
const maxAvailableNames = 100

func capNames(names []string) []string {
	sort.Strings(names)
	if len(names) > maxAvailableNames {
		names = append(names[:maxAvailableNames:maxAvailableNames], "...")
	}
	return names
}
