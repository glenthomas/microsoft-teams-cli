package graph

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Team is a Microsoft Teams team.
type Team struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// Channel is a channel within a team.
type Channel struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Description    string `json:"description"`
	MembershipType string `json:"membershipType"`
	WebURL         string `json:"webUrl"`
}

// Chat is a one-to-one, group, or meeting chat.
type Chat struct {
	ID                  string     `json:"id"`
	ChatType            string     `json:"chatType"`
	Topic               string     `json:"topic"`
	LastUpdatedDateTime *time.Time `json:"lastUpdatedDateTime"`
}

// Identity is a user, application or device identity.
type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// IdentitySet identifies the sender of a message.
type IdentitySet struct {
	User        *Identity `json:"user"`
	Application *Identity `json:"application"`
	Device      *Identity `json:"device"`
}

// ItemBody is the body of a message.
type ItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

// Attachment is a file, card or other item attached to a message.
type Attachment struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	ContentURL  string `json:"contentUrl"`
	Name        string `json:"name"`
}

// ChatMessage is a message in a chat or channel.
type ChatMessage struct {
	ID                   string        `json:"id"`
	ChatID               string        `json:"chatId"`
	ReplyToID            string        `json:"replyToId"`
	MessageType          string        `json:"messageType"`
	CreatedDateTime      time.Time     `json:"createdDateTime"`
	LastModifiedDateTime *time.Time    `json:"lastModifiedDateTime"`
	DeletedDateTime      *time.Time    `json:"deletedDateTime"`
	Subject              string        `json:"subject"`
	Importance           string        `json:"importance"`
	WebURL               string        `json:"webUrl"`
	From                 *IdentitySet  `json:"from"`
	Body                 ItemBody      `json:"body"`
	Attachments          []Attachment  `json:"attachments"`
	Replies              []ChatMessage `json:"replies"`
}

// LatestActivity returns the most recent timestamp on the message or any of
// its (expanded) replies.
func (m ChatMessage) LatestActivity() time.Time {
	t := m.CreatedDateTime
	if m.LastModifiedDateTime != nil && m.LastModifiedDateTime.After(t) {
		t = *m.LastModifiedDateTime
	}
	for _, r := range m.Replies {
		if a := r.LatestActivity(); a.After(t) {
			t = a
		}
	}
	return t
}

// User is the signed-in user profile.
type User struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	UserPrincipalName string `json:"userPrincipalName"`
	Mail              string `json:"mail"`
}

// Me returns the signed-in user's profile.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	err := c.Get(ctx, "/me", url.Values{"$select": {"id,displayName,userPrincipalName,mail"}}, &u)
	return u, err
}

// JoinedTeams lists the teams the signed-in user is a member of.
func (c *Client) JoinedTeams(ctx context.Context) ([]Team, error) {
	return All[Team](ctx, c, "/me/joinedTeams", url.Values{"$select": {"id,displayName,description"}})
}

// Team fetches a single team by ID.
func (c *Client) Team(ctx context.Context, teamID string) (Team, error) {
	var t Team
	err := c.Get(ctx, "/teams/"+url.PathEscape(teamID), url.Values{"$select": {"id,displayName,description"}}, &t)
	return t, err
}

// Channels lists the channels of a team.
func (c *Client) Channels(ctx context.Context, teamID string) ([]Channel, error) {
	return All[Channel](ctx, c, "/teams/"+url.PathEscape(teamID)+"/channels", nil)
}

// PrivateChats lists the signed-in user's one-to-one and group chats.
func (c *Client) PrivateChats(ctx context.Context) ([]Chat, error) {
	chats, err := All[Chat](ctx, c, "/me/chats", url.Values{"$select": {"id,chatType,topic,lastUpdatedDateTime"}})
	if err != nil {
		return nil, err
	}
	private := make([]Chat, 0, len(chats))
	for _, chat := range chats {
		if chat.ChatType == "oneOnOne" || chat.ChatType == "group" {
			private = append(private, chat)
		}
	}
	return private, nil
}

// ChatMessages returns all messages in a chat, newest activity first.
func (c *Client) ChatMessages(ctx context.Context, chatID string) ([]ChatMessage, error) {
	return All[ChatMessage](ctx, c, chatMessagesPath(chatID), url.Values{"$top": {"50"}})
}

// PostChatMessage creates a message in a one-to-one or group chat.
func (c *Client) PostChatMessage(ctx context.Context, chatID, content string) (ChatMessage, error) {
	return c.postMessage(ctx, chatMessagesPath(chatID), content)
}

// ChannelMessagePages iterates over root messages in a channel (newest
// threads first), with replies expanded, calling fn for each page.
func (c *Client) ChannelMessagePages(ctx context.Context, teamID, channelID string, pageSize int, fn func([]ChatMessage) bool) error {
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 50
	}
	q := url.Values{
		"$top":    {strconv.Itoa(pageSize)},
		"$expand": {"replies"},
	}
	return Pages(ctx, c, channelMessagesPath(teamID, channelID), q, fn)
}

// ChannelMessage fetches a single root message.
func (c *Client) ChannelMessage(ctx context.Context, teamID, channelID, messageID string) (ChatMessage, error) {
	var m ChatMessage
	err := c.Get(ctx, channelMessagesPath(teamID, channelID)+"/"+url.PathEscape(messageID), nil, &m)
	return m, err
}

// Replies lists all replies to a root message.
func (c *Client) Replies(ctx context.Context, teamID, channelID, messageID string) ([]ChatMessage, error) {
	return All[ChatMessage](ctx, c, channelMessagesPath(teamID, channelID)+"/"+url.PathEscape(messageID)+"/replies",
		url.Values{"$top": {"50"}})
}

// PostChannelMessage creates a root message in a channel.
func (c *Client) PostChannelMessage(ctx context.Context, teamID, channelID, content string) (ChatMessage, error) {
	return c.postMessage(ctx, channelMessagesPath(teamID, channelID), content)
}

// ReplyToChannelMessage creates a reply to a root channel message.
func (c *Client) ReplyToChannelMessage(ctx context.Context, teamID, channelID, messageID, content string) (ChatMessage, error) {
	path := channelMessagesPath(teamID, channelID) + "/" + url.PathEscape(messageID) + "/replies"
	return c.postMessage(ctx, path, content)
}

func (c *Client) postMessage(ctx context.Context, path, content string) (ChatMessage, error) {
	var message ChatMessage
	err := c.Post(ctx, path, struct {
		Body ItemBody `json:"body"`
	}{Body: ItemBody{ContentType: "text", Content: content}}, &message)
	return message, err
}

func channelMessagesPath(teamID, channelID string) string {
	return "/teams/" + url.PathEscape(teamID) + "/channels/" + url.PathEscape(channelID) + "/messages"
}

func chatMessagesPath(chatID string) string {
	return "/chats/" + url.PathEscape(chatID) + "/messages"
}
