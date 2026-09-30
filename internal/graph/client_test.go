package graph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type tok string

func (t tok) Token(context.Context) (string, error) { return string(t), nil }

func TestGetRetriesOnThrottling(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+"abc" {
			t.Errorf("missing bearer token")
		}
		if calls < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","displayName":"Jane"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, tok("abc"))
	var slept []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.DisplayName != "Jane" || calls != 3 || len(slept) != 2 || slept[0] != time.Second {
		t.Fatalf("me=%+v calls=%d slept=%v", me, calls, slept)
	}
}

func TestGetReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"Forbidden","message":"Missing scope"}}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, tok("abc")).Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 || apiErr.Code != "Forbidden" || apiErr.Message != "Missing scope" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if !IsStatus(err, 403) {
		t.Fatal("IsStatus should report 403")
	}
}

func TestPostSendsJSONAndDecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/teams/team-1/channels/channel-1/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer abc" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing auth/content headers: %v", r.Header)
		}
		var request struct {
			Body ItemBody `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Body.ContentType != "text" || request.Body.Content != "hello" {
			t.Errorf("unexpected request body: %+v", request.Body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"new-1","body":{"contentType":"text","content":"hello"}}`))
	}))
	defer srv.Close()

	message, err := NewClient(srv.URL, tok("abc")).PostChannelMessage(context.Background(), "team-1", "channel-1", "hello")
	if err != nil || message.ID != "new-1" || message.Body.Content != "hello" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
}

func TestResolveKeepsODataDollarAndRejectsForeignHosts(t *testing.T) {
	c := NewClient("https://graph.microsoft.com/v1.0", tok("x"))
	u, err := c.resolve("/teams/a/channels/19:abc@thread.tacv2/messages", url.Values{"$top": {"50"}, "$expand": {"replies"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "$top=50") || !strings.Contains(u, "$expand=replies") || !strings.Contains(u, "/channels/19:abc@thread.tacv2/messages") {
		t.Fatalf("unexpected url %s", u)
	}
	if _, err := c.resolve("https://evil.example.com/v1.0/me", nil); err == nil {
		t.Fatal("expected foreign host to be rejected")
	}
	if _, err := c.resolve("https://graph.microsoft.com/v1.0/me?$skiptoken=x", nil); err != nil {
		t.Fatal(err)
	}
}
