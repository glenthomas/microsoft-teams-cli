package teams

import (
	"context"
	"sort"
	"time"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/query"
	"github.com/glenthomas/microsoft-teams-cli/internal/textutil"
)

// Message is the flattened, agent-friendly representation of a channel
// message or reply.
type Message struct {
	ID                   string       `json:"id"`
	Type                 string       `json:"type"` // "message" or "reply"
	ThreadID             string       `json:"threadId"`
	TeamID               string       `json:"teamId"`
	TeamName             string       `json:"teamName"`
	ChannelID            string       `json:"channelId"`
	ChannelName          string       `json:"channelName"`
	Author               string       `json:"author,omitempty"`
	AuthorID             string       `json:"authorId,omitempty"`
	CreatedDateTime      time.Time    `json:"createdDateTime"`
	LastModifiedDateTime *time.Time   `json:"lastModifiedDateTime,omitempty"`
	Subject              string       `json:"subject,omitempty"`
	Importance           string       `json:"importance,omitempty"`
	Text                 string       `json:"text"`
	WebURL               string       `json:"webUrl,omitempty"`
	ReplyCount           *int         `json:"replyCount,omitempty"`
	Attachments          []Attachment `json:"attachments,omitempty"`
}

// Attachment is a simplified message attachment.
type Attachment struct {
	Name        string `json:"name,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	URL         string `json:"url,omitempty"`
}

// NewMessage converts a Graph message into a Message.
func NewMessage(ch ChannelRef, m graph.ChatMessage, threadID string) Message {
	out := Message{
		ID:                   m.ID,
		Type:                 "message",
		ThreadID:             threadID,
		TeamID:               ch.TeamID,
		TeamName:             ch.TeamName,
		ChannelID:            ch.ChannelID,
		ChannelName:          ch.ChannelName,
		CreatedDateTime:      m.CreatedDateTime,
		LastModifiedDateTime: m.LastModifiedDateTime,
		Subject:              m.Subject,
		Text:                 textutil.BodyToText(m.Body.ContentType, m.Body.Content),
		WebURL:               m.WebURL,
	}
	if m.ReplyToID != "" || m.ID != threadID {
		out.Type = "reply"
	}
	if m.Importance != "" && m.Importance != "normal" {
		out.Importance = m.Importance
	}
	if m.From != nil {
		for _, id := range []*graph.Identity{m.From.User, m.From.Application, m.From.Device} {
			if id != nil {
				out.Author, out.AuthorID = id.DisplayName, id.ID
				break
			}
		}
	}
	for _, a := range m.Attachments {
		// Inline message references carry no useful standalone content.
		if a.ContentType == "messageReference" {
			continue
		}
		out.Attachments = append(out.Attachments, Attachment{Name: a.Name, ContentType: a.ContentType, URL: a.ContentURL})
	}
	return out
}

// isUserMessage reports whether m is a regular, non-deleted message.
func isUserMessage(m graph.ChatMessage) bool {
	return m.DeletedDateTime == nil && (m.MessageType == "" || m.MessageType == "message")
}

// CollectOptions controls which messages are collected from a channel.
type CollectOptions struct {
	// Since and Until bound message creation time; zero values are unbounded.
	Since, Until time.Time
	// Matcher filters messages by text; an empty matcher matches all.
	Matcher query.Matcher
	// IncludeReplies includes replies in addition to root messages.
	IncludeReplies bool
	// MaxThreads caps the number of threads scanned per channel (0 = no cap).
	MaxThreads int
}

// CollectResult is the outcome of scanning one channel.
type CollectResult struct {
	Messages       []Message
	ThreadsScanned int
	// Truncated is set when MaxThreads was reached before the time window
	// was exhausted, so older matches may be missing.
	Truncated bool
}

func (o CollectOptions) inWindow(t time.Time) bool {
	if !o.Since.IsZero() && t.Before(o.Since) {
		return false
	}
	if !o.Until.IsZero() && !t.Before(o.Until) {
		return false
	}
	return true
}

// Collect scans a channel's threads (newest first) and returns messages that
// fall in the time window and match the query. Scanning stops once a whole
// page of threads has had no activity since opts.Since.
func (s *Service) Collect(ctx context.Context, ch ChannelRef, opts CollectOptions) (CollectResult, error) {
	var res CollectResult
	err := s.Graph.ChannelMessagePages(ctx, ch.TeamID, ch.ChannelID, 50, func(page []graph.ChatMessage) bool {
		active := false
		for _, root := range page {
			if !opts.Since.IsZero() && root.LatestActivity().Before(opts.Since) {
				continue
			}
			active = true
			if opts.MaxThreads > 0 && res.ThreadsScanned >= opts.MaxThreads {
				res.Truncated = true
				return false
			}
			res.ThreadsScanned++
			res.Messages = append(res.Messages, matchThread(ch, root, opts)...)
		}
		return active || opts.Since.IsZero() && len(page) > 0
	})
	if err != nil {
		return res, err
	}
	SortNewestFirst(res.Messages)
	return res, nil
}

func matchThread(ch ChannelRef, root graph.ChatMessage, opts CollectOptions) []Message {
	var out []Message
	consider := func(m graph.ChatMessage, isRoot bool) {
		if !isUserMessage(m) || !opts.inWindow(m.CreatedDateTime) {
			return
		}
		msg := NewMessage(ch, m, root.ID)
		if !opts.Matcher.Match(msg.Subject, msg.Text) {
			return
		}
		if isRoot {
			n := countUserMessages(root.Replies)
			msg.ReplyCount = &n
		}
		out = append(out, msg)
	}
	consider(root, true)
	if opts.IncludeReplies {
		for _, r := range root.Replies {
			consider(r, false)
		}
	}
	return out
}

func countUserMessages(ms []graph.ChatMessage) int {
	n := 0
	for _, m := range ms {
		if isUserMessage(m) {
			n++
		}
	}
	return n
}

// SortNewestFirst orders messages by creation time, newest first.
func SortNewestFirst(ms []Message) {
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].CreatedDateTime.After(ms[j].CreatedDateTime) })
}

// Thread returns a root message and all of its replies, oldest first.
func (s *Service) Thread(ctx context.Context, ch ChannelRef, messageID string) ([]Message, error) {
	root, err := s.Graph.ChannelMessage(ctx, ch.TeamID, ch.ChannelID, messageID)
	if err != nil {
		return nil, err
	}
	// Allow a reply ID to be passed: fetch its root thread instead.
	if root.ReplyToID != "" && root.ReplyToID != root.ID {
		return s.Thread(ctx, ch, root.ReplyToID)
	}
	replies, err := s.Graph.Replies(ctx, ch.TeamID, ch.ChannelID, root.ID)
	if err != nil {
		return nil, err
	}
	var out []Message
	if isUserMessage(root) {
		m := NewMessage(ch, root, root.ID)
		n := countUserMessages(replies)
		m.ReplyCount = &n
		out = append(out, m)
	}
	for _, r := range replies {
		if isUserMessage(r) {
			out = append(out, NewMessage(ch, r, root.ID))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedDateTime.Before(out[j].CreatedDateTime) })
	return out, nil
}
