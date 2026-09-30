package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph"
	"github.com/glenthomas/microsoft-teams-cli/internal/graph/graphtest"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func run(t *testing.T, srv *graphtest.Server, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := &App{
		Stdout: &stdout, Stderr: &stderr,
		Now:      func() time.Time { return now },
		NewGraph: func(context.Context) (*graph.Client, error) { return srv.Client(), nil },
	}
	code := app.Run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

func seeded(t *testing.T) *graphtest.Server {
	srv := graphtest.New(t)
	srv.Seed(now)
	return srv
}

func TestSearchJSON(t *testing.T) {
	srv := seeded(t)
	code, out, errOut := run(t, srv, "search", "--channel", "platform-engineering", "--since", "30d", "--query", "private endpoints")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var res struct {
		Terms    []string `json:"terms"`
		Since    time.Time
		Channels []struct{ ChannelName string }
		Count    int
		Messages []struct {
			ID, Type, ThreadID, Author, Text, ChannelName, TeamName string
		}
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if res.Count != 3 || len(res.Messages) != 3 || res.Messages[0].ID != "r1" || res.Messages[2].ID != "r3" {
		t.Fatalf("unexpected result: %s", out)
	}
	if !res.Since.Equal(now.AddDate(0, 0, -30)) || len(res.Terms) != 2 || res.Channels[0].ChannelName != "Platform Engineering" {
		t.Fatalf("unexpected metadata: %s", out)
	}
	if m := res.Messages[1]; m.Type != "message" || m.Author != "Alice" || m.TeamName != "Platform" || !strings.Contains(m.Text, "Private Endpoints") {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestSearchLimitAndText(t *testing.T) {
	srv := seeded(t)
	code, out, _ := run(t, srv, "search", "-c", "Platform Engineering", "-q", "private endpoints", "-n", "1", "--format", "text")
	if code != ExitOK || !strings.Contains(out, "Bob in Platform/Platform Engineering (reply)") || strings.Contains(out, "Alice") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}

	code, out, _ = run(t, srv, "search", "-c", "Platform Engineering", "-q", "private endpoints", "-n", "1")
	if code != ExitOK || !strings.Contains(out, `"truncated": true`) {
		t.Fatalf("expected truncated output, got %s", out)
	}
}

func TestMessagesAcrossTeamSkipsForbiddenChannels(t *testing.T) {
	srv := seeded(t)
	srv.Forbidden["19:gen1@thread.tacv2"] = true
	code, out, errOut := run(t, srv, "messages", "--team", "Platform", "--since", "7d")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "skipped Platform/General") || !strings.Contains(out, `"id": "m1"`) || strings.Contains(out, `"id": "m2"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestThreadCommand(t *testing.T) {
	srv := seeded(t)
	code, out, errOut := run(t, srv, "thread", "--channel", "platform-engineering", "--id", "m1")
	if code != ExitOK || !strings.Contains(out, `"count": 3`) || !strings.Contains(out, `"threadId": "m1"`) {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
}

func TestListCommands(t *testing.T) {
	srv := seeded(t)
	if code, out, _ := run(t, srv, "teams"); code != ExitOK || !strings.Contains(out, `"count": 2`) {
		t.Fatalf("teams: %d %s", code, out)
	}
	if code, out, _ := run(t, srv, "channels", "--team", "Platform"); code != ExitOK || !strings.Contains(out, "19:pe@thread.tacv2") || strings.Contains(out, "gen2") {
		t.Fatalf("channels: %d %s", code, out)
	}
	if code, out, _ := run(t, srv, "whoami"); code != ExitOK || !strings.Contains(out, "test@example.com") {
		t.Fatalf("whoami: %d %s", code, out)
	}
}

func errorCode(t *testing.T, stderr string) string {
	t.Helper()
	var e struct{ Error ErrorInfo }
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr)
	}
	return e.Error.Code
}

func TestErrors(t *testing.T) {
	srv := seeded(t)
	cases := []struct {
		args     []string
		exit     int
		errCode  string
		errMatch string
	}{
		{[]string{"search", "--channel", "general", "--query", "x"}, ExitNotFound, "ambiguous", "--team"},
		{[]string{"search", "--channel", "nope", "--query", "x"}, ExitNotFound, "not_found", "Platform Engineering"},
		{[]string{"search", "--channel", "general"}, ExitUsage, "usage", "--query is required"},
		{[]string{"search", "--query", "x"}, ExitUsage, "usage", "--channel or --team"},
		{[]string{"messages", "-c", "general", "-t", "data", "--since", "soon"}, ExitUsage, "usage", "invalid since"},
		{[]string{"messages", "-c", "general", "-t", "data", "--since", "1d", "--until", "2d"}, ExitUsage, "usage", "--until must be after"},
		{[]string{"teams", "--format", "xml"}, ExitUsage, "usage", "invalid --format"},
		{[]string{"bogus"}, ExitUsage, "usage", "unknown command"},
		{[]string{"login", "--timeout", "0s"}, ExitUsage, "usage", "--timeout must be positive"},
		{[]string{"thread", "--channel", "platform-engineering", "--id", "missing"}, ExitNotFound, "not_found", "message not found"},
	}
	for _, c := range cases {
		code, out, errOut := run(t, srv, c.args...)
		if code != c.exit || out != "" || errorCode(t, errOut) != c.errCode || !strings.Contains(errOut, c.errMatch) {
			t.Errorf("%v: exit=%d stdout=%q stderr=%s", c.args, code, out, errOut)
		}
	}
}

func TestUnauthorizedMapsToAuthExitCode(t *testing.T) {
	srv := seeded(t)
	var stdout, stderr bytes.Buffer
	app := &App{
		Stdout: &stdout, Stderr: &stderr,
		NewGraph: func(context.Context) (*graph.Client, error) {
			c := srv.Client()
			c.Tokens = badToken{}
			return c, nil
		},
	}
	if code := app.Run(context.Background(), []string{"whoami"}); code != ExitAuth || errorCode(t, stderr.String()) != "not_logged_in" {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

type badToken struct{}

func (badToken) Token(context.Context) (string, error) { return "expired", nil }

func TestClassifyTimeout(t *testing.T) {
	info, code := classify(fmt.Errorf("sign-in failed: %w", context.DeadlineExceeded))
	if info.Code != "timeout" || code != ExitError {
		t.Fatalf("got %s/%d", info.Code, code)
	}
}
