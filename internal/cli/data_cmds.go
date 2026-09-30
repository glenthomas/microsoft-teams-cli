package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/query"
	"github.com/glenthomas/microsoft-teams-cli/internal/teams"
)

func (a *App) teamsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "teams",
		Short: "List the teams you are a member of",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := a.service(cmd.Context())
			if err != nil {
				return err
			}
			ts, err := svc.ResolveTeams(cmd.Context(), "")
			if err != nil {
				return err
			}
			if ts == nil {
				ts = []graph.Team{}
			}
			return a.emit(map[string]any{"count": len(ts), "teams": ts}, func(w io.Writer) {
				for _, t := range ts {
					fmt.Fprintf(w, "%s\t%s\n", t.ID, t.DisplayName)
				}
			})
		},
	}
}

func (a *App) channelsCmd() *cobra.Command {
	var team string
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List channels (in one team, or across all your teams)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := a.service(cmd.Context())
			if err != nil {
				return err
			}
			chans, err := svc.ListChannels(cmd.Context(), team)
			if err != nil {
				return err
			}
			if chans == nil {
				chans = []teams.ChannelRef{}
			}
			return a.emit(map[string]any{"count": len(chans), "channels": chans}, func(w io.Writer) {
				for _, c := range chans {
					fmt.Fprintf(w, "%s\t%s\t%s\n", c.TeamName, c.ChannelName, c.ChannelID)
				}
			})
		},
	}
	cmd.Flags().StringVarP(&team, "team", "t", "", "team name or ID (default: all joined teams)")
	return cmd
}

// messageFlags are shared by the messages and search commands.
type messageFlags struct {
	team, channel, since, until, query string
	limit, maxThreads                  int
	noReplies                          bool
}

func (f *messageFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVarP(&f.channel, "channel", "c", "", "channel name or ID; names match case-insensitively, ignoring spaces/punctuation")
	fl.StringVarP(&f.team, "team", "t", "", "team name or ID (optional; narrows channel lookup, or searches all its channels if --channel is omitted)")
	fl.StringVarP(&f.since, "since", "s", "", "only messages created after this time: relative (30d, 12h, 2w, 3mo) or date/RFC 3339 timestamp")
	fl.StringVar(&f.until, "until", "", "only messages created before this time (same formats as --since)")
	fl.IntVarP(&f.limit, "limit", "n", 50, "maximum number of messages to return (0 = no limit)")
	fl.IntVar(&f.maxThreads, "max-threads", 1000, "maximum threads to scan per channel (0 = no limit)")
	fl.BoolVar(&f.noReplies, "no-replies", false, "only include root messages, not thread replies")
}

// messagesResult is the JSON document returned by messages and search.
type messagesResult struct {
	Query     string             `json:"query,omitempty"`
	Terms     []string           `json:"terms,omitempty"`
	Since     *time.Time         `json:"since,omitempty"`
	Until     *time.Time         `json:"until,omitempty"`
	Channels  []teams.ChannelRef `json:"channels"`
	Count     int                `json:"count"`
	Truncated bool               `json:"truncated"`
	Warnings  []string           `json:"warnings,omitempty"`
	Messages  []teams.Message    `json:"messages"`
}

func (a *App) runMessages(ctx context.Context, f *messageFlags) error {
	if f.channel == "" && f.team == "" {
		return newUsageError(fmt.Errorf("--channel or --team is required"))
	}
	if f.limit < 0 || f.maxThreads < 0 {
		return newUsageError(fmt.Errorf("--limit and --max-threads must not be negative"))
	}
	now := a.Now().UTC().Truncate(time.Second)
	since, err := query.ParseSince(f.since, now)
	if err != nil {
		return newUsageError(err)
	}
	until, err := query.ParseSince(f.until, now)
	if err != nil {
		return newUsageError(errors.New(strings.Replace(err.Error(), "since", "until", 1)))
	}
	if !since.IsZero() && !until.IsZero() && !until.After(since) {
		return newUsageError(fmt.Errorf("--until must be after --since"))
	}

	svc, err := a.service(ctx)
	if err != nil {
		return err
	}
	chans, err := svc.ResolveChannels(ctx, f.team, f.channel)
	if err != nil {
		return err
	}

	matcher := query.NewMatcher(f.query)
	opts := teams.CollectOptions{
		Since: since, Until: until, Matcher: matcher,
		IncludeReplies: !f.noReplies, MaxThreads: f.maxThreads,
	}
	res := messagesResult{Query: f.query, Terms: matcher.Terms(), Channels: chans, Messages: []teams.Message{}}
	if !since.IsZero() {
		res.Since = &since
	}
	if !until.IsZero() {
		res.Until = &until
	}
	for _, ch := range chans {
		cr, err := svc.Collect(ctx, ch, opts)
		if err != nil {
			// When scanning a whole team, skip channels the user cannot read.
			if f.channel == "" && (graph.IsStatus(err, 403) || graph.IsStatus(err, 404)) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped %s/%s: %v", ch.TeamName, ch.ChannelName, err))
				continue
			}
			return err
		}
		if cr.Truncated {
			res.Truncated = true
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s/%s: stopped after scanning %d threads (--max-threads); older messages were not searched", ch.TeamName, ch.ChannelName, cr.ThreadsScanned))
		}
		res.Messages = append(res.Messages, cr.Messages...)
	}
	teams.SortNewestFirst(res.Messages)
	if f.limit > 0 && len(res.Messages) > f.limit {
		res.Messages = res.Messages[:f.limit]
		res.Truncated = true
	}
	res.Count = len(res.Messages)
	return a.emit(res, func(w io.Writer) { writeMessagesText(w, res.Messages, res.Warnings) })
}

func (a *App) messagesCmd() *cobra.Command {
	f := &messageFlags{}
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "List recent messages in a channel (newest first)",
		Example: `  teams messages --channel platform-engineering --since 7d
  teams messages --team Platform --since 24h --no-replies`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runMessages(cmd.Context(), f) },
	}
	f.register(cmd)
	return cmd
}

func (a *App) searchCmd() *cobra.Command {
	f := &messageFlags{}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search channel messages and replies for text (newest first)",
		Long: `Search messages and thread replies in a channel (or every channel in a team).

The query is matched case-insensitively against the message subject and plain
text. All words must be present (AND); wrap words in double quotes to match an
exact phrase, e.g. --query '"private endpoint" dns'.`,
		Example: `  teams search --channel platform-engineering --since 30d --query "private endpoints"
  teams search --team Platform --since 2w --query '"terraform plan" error'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(f.query) == "" {
				return newUsageError(fmt.Errorf("--query is required"))
			}
			return a.runMessages(cmd.Context(), f)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVarP(&f.query, "query", "q", "", "text to search for (required)")
	return cmd
}

func (a *App) threadCmd() *cobra.Command {
	var team, channel, id string
	cmd := &cobra.Command{
		Use:     "thread",
		Short:   "Show a message thread (root message and all replies, oldest first)",
		Example: `  teams thread --channel platform-engineering --id 1717171717171`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if channel == "" || id == "" {
				return newUsageError(fmt.Errorf("--channel and --id are required"))
			}
			svc, err := a.service(cmd.Context())
			if err != nil {
				return err
			}
			chans, err := svc.ResolveChannels(cmd.Context(), team, channel)
			if err != nil {
				return err
			}
			ch := chans[0]
			msgs, err := svc.Thread(cmd.Context(), ch, id)
			if err != nil {
				return err
			}
			if msgs == nil {
				msgs = []teams.Message{}
			}
			threadID := id
			if len(msgs) > 0 {
				threadID = msgs[0].ThreadID
			}
			out := map[string]any{"channel": ch, "threadId": threadID, "count": len(msgs), "messages": msgs}
			return a.emit(out, func(w io.Writer) { writeMessagesText(w, msgs, nil) })
		},
	}
	cmd.Flags().StringVarP(&channel, "channel", "c", "", "channel name or ID (required)")
	cmd.Flags().StringVarP(&team, "team", "t", "", "team name or ID (optional)")
	cmd.Flags().StringVar(&id, "id", "", "message ID of the thread's root message (the threadId field from search/messages output)")
	return cmd
}

func writeMessagesText(w io.Writer, msgs []teams.Message, warnings []string) {
	for _, warn := range warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	for i, m := range msgs {
		if i > 0 {
			fmt.Fprintln(w)
		}
		kind := ""
		if m.Type == "reply" {
			kind = " (reply)"
		}
		fmt.Fprintf(w, "[%s] %s in %s/%s%s\n", m.CreatedDateTime.Local().Format("2006-01-02 15:04"), m.Author, m.TeamName, m.ChannelName, kind)
		if m.Subject != "" {
			fmt.Fprintf(w, "Subject: %s\n", m.Subject)
		}
		fmt.Fprintln(w, m.Text)
		if m.WebURL != "" {
			fmt.Fprintln(w, m.WebURL)
		}
	}
}
