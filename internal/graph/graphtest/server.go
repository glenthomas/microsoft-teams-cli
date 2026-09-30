// Package graphtest provides an in-memory fake of the Microsoft Graph Teams
// endpoints used by the CLI, for tests.
package graphtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
)

// Token is the bearer token the fake server accepts.
const Token = "test-token"

// Server is a fake Graph server.
type Server struct {
	*httptest.Server
	Me       graph.User
	Teams    []graph.Team
	Channels map[string][]graph.Channel     // by team ID
	Messages map[string][]graph.ChatMessage // root messages (with replies) by channel ID
	// Forbidden channel IDs return 403 for message requests.
	Forbidden map[string]bool
	// PageSize overrides the page size for message listings (default: $top).
	PageSize int

	mu       sync.Mutex
	requests []string
}

// New starts a fake Graph server that is closed when the test ends.
func New(t *testing.T) *Server {
	s := &Server{
		Channels:  map[string][]graph.Channel{},
		Messages:  map[string][]graph.ChatMessage{},
		Forbidden: map[string]bool{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// Client returns a Graph client pointed at the fake server.
func (s *Server) Client() *graph.Client {
	return graph.NewClient(s.URL+"/v1.0", staticToken(Token))
}

// Requests returns the request URIs received so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.RequestURI())
	s.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer "+Token {
		writeErr(w, http.StatusUnauthorized, "InvalidAuthenticationToken", "Access token is empty.")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1.0"), "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "me":
		writeJSON(w, s.Me)
	case len(parts) == 2 && parts[0] == "me" && parts[1] == "joinedTeams":
		writeJSON(w, map[string]any{"value": s.Teams})
	case len(parts) == 2 && parts[0] == "teams":
		for _, t := range s.Teams {
			if t.ID == parts[1] {
				writeJSON(w, t)
				return
			}
		}
		writeErr(w, http.StatusNotFound, "NotFound", "team not found")
	case len(parts) == 3 && parts[0] == "teams" && parts[2] == "channels":
		chans, ok := s.Channels[parts[1]]
		if !ok {
			writeErr(w, http.StatusNotFound, "NotFound", "team not found")
			return
		}
		writeJSON(w, map[string]any{"value": chans})
	case len(parts) >= 5 && parts[0] == "teams" && parts[2] == "channels" && parts[4] == "messages":
		s.handleMessages(w, r, parts[3], parts[5:])
	default:
		writeErr(w, http.StatusNotFound, "UnknownPath", r.URL.Path)
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request, channelID string, rest []string) {
	if s.Forbidden[channelID] {
		writeErr(w, http.StatusForbidden, "Forbidden", "Missing role permissions on the request.")
		return
	}
	msgs, ok := s.Messages[channelID]
	if !ok {
		writeErr(w, http.StatusNotFound, "NotFound", "channel not found")
		return
	}
	find := func(id string) (graph.ChatMessage, bool) {
		for _, m := range msgs {
			if m.ID == id {
				return m, true
			}
		}
		return graph.ChatMessage{}, false
	}
	q := r.URL.Query()
	switch len(rest) {
	case 0:
		size := s.PageSize
		if size == 0 {
			size, _ = strconv.Atoi(q.Get("$top"))
		}
		if size <= 0 {
			size = 20
		}
		skip, _ := strconv.Atoi(q.Get("$skiptoken"))
		end := min(skip+size, len(msgs))
		page := make([]graph.ChatMessage, 0, end-skip)
		for _, m := range msgs[skip:end] {
			if q.Get("$expand") != "replies" {
				m.Replies = nil
			}
			page = append(page, m)
		}
		resp := map[string]any{"value": page}
		if end < len(msgs) {
			resp["@odata.nextLink"] = s.URL + r.URL.EscapedPath() + "?$top=" + strconv.Itoa(size) +
				"&$expand=" + q.Get("$expand") + "&$skiptoken=" + strconv.Itoa(end)
		}
		writeJSON(w, resp)
	case 1:
		m, ok := find(rest[0])
		if !ok {
			writeErr(w, http.StatusNotFound, "NotFound", "message not found")
			return
		}
		m.Replies = nil
		writeJSON(w, m)
	case 2:
		m, ok := find(rest[0])
		if !ok || rest[1] != "replies" {
			writeErr(w, http.StatusNotFound, "NotFound", "message not found")
			return
		}
		writeJSON(w, map[string]any{"value": m.Replies})
	default:
		writeErr(w, http.StatusNotFound, "UnknownPath", r.URL.Path)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
