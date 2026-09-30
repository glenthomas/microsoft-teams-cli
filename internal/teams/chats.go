package teams

import (
	"context"
	"time"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/textutil"
)

// ChatMessage is a plain-text representation of a private chat message.
type ChatMessage struct {
	ID                   string     `json:"id"`
	ChatID               string     `json:"chatId"`
	ReplyToID            string     `json:"replyToId,omitempty"`
	Author               string     `json:"author,omitempty"`
	AuthorID             string     `json:"authorId,omitempty"`
	CreatedDateTime      time.Time  `json:"createdDateTime"`
	LastModifiedDateTime *time.Time `json:"lastModifiedDateTime,omitempty"`
	Text                 string     `json:"text"`
	WebURL               string     `json:"webUrl,omitempty"`
}

// ListPrivateChats returns the signed-in user's one-to-one and group chats.
func (s *Service) ListPrivateChats(ctx context.Context) ([]graph.Chat, error) {
	return s.Graph.PrivateChats(ctx)
}

// ChatMessages returns all messages in a private chat, newest first.
func (s *Service) ChatMessages(ctx context.Context, chatID string) ([]ChatMessage, error) {
	items, err := s.Graph.ChatMessages(ctx, chatID)
	if err != nil {
		return nil, err
	}
	messages := make([]ChatMessage, 0, len(items))
	for _, item := range items {
		if item.DeletedDateTime != nil || item.MessageType != "" && item.MessageType != "message" {
			continue
		}
		messages = append(messages, newChatMessage(item))
	}
	return messages, nil
}

// PostChatMessage sends a message to a one-to-one or group chat.
func (s *Service) PostChatMessage(ctx context.Context, chatID, content string) (ChatMessage, error) {
	created, err := s.Graph.PostChatMessage(ctx, chatID, content)
	if err != nil {
		return ChatMessage{}, err
	}
	return newChatMessage(created), nil
}

func newChatMessage(m graph.ChatMessage) ChatMessage {
	out := ChatMessage{
		ID: m.ID, ChatID: m.ChatID, ReplyToID: m.ReplyToID,
		CreatedDateTime: m.CreatedDateTime, LastModifiedDateTime: m.LastModifiedDateTime,
		Text: textutil.BodyToText(m.Body.ContentType, m.Body.Content), WebURL: m.WebURL,
	}
	if m.From != nil {
		for _, id := range []*graph.Identity{m.From.User, m.From.Application, m.From.Device} {
			if id != nil {
				out.Author, out.AuthorID = id.DisplayName, id.ID
				break
			}
		}
	}
	return out
}
