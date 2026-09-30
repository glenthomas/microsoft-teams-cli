package teams

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glenthomas/microsoft-teams-cli/internal/graph/graphtest"
	"github.com/glenthomas/microsoft-teams-cli/internal/query"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T) (*Service, *graphtest.Server) {
	srv := graphtest.New(t)
	srv.Seed(now)
	return &Service{Graph: srv.Client()}, srv
}

func TestResolveChannels(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	chans, err := svc.ResolveChannels(ctx, "", "platform-engineering")
	if err != nil || len(chans) != 1 || chans[0].ChannelID != "19:pe@thread.tacv2" || chans[0].TeamName != "Platform" {
		t.Fatalf("normalized name: %+v %v", chans, err)
	}
	chans, err = svc.ResolveChannels(ctx, "", "19:gen2@thread.tacv2")
	if err != nil || len(chans) != 1 || chans[0].TeamID != "team-2" {
		t.Fatalf("by id: %+v %v", chans, err)
	}
	chans, err = svc.ResolveChannels(ctx, "data", "general")
	if err != nil || len(chans) != 1 || chans[0].ChannelID != "19:gen2@thread.tacv2" {
		t.Fatalf("with team: %+v %v", chans, err)
	}
	chans, err = svc.ResolveChannels(ctx, "team-1", "")
	if err != nil || len(chans) != 2 {
		t.Fatalf("whole team: %+v %v", chans, err)
	}

	var amb *AmbiguousError
	if _, err := svc.ResolveChannels(ctx, "", "General"); !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
	var nf *NotFoundError
	if _, err := svc.ResolveChannels(ctx, "", "nope"); !errors.As(err, &nf) || len(nf.Available) != 2 {
		t.Fatalf("expected not found with suggestions, got %v", err)
	}
	if _, err := svc.ResolveChannels(ctx, "missing-team", "general"); !errors.As(err, &nf) || nf.Kind != "team" {
		t.Fatalf("expected team not found, got %v", err)
	}
}

func ids(ms []Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestCollectSearch(t *testing.T) {
	svc, srv := newService(t)
	srv.PageSize = 2
	ch := ChannelRef{TeamID: "team-1", TeamName: "Platform", ChannelID: "19:pe@thread.tacv2", ChannelName: "Platform Engineering"}

	res, err := svc.Collect(context.Background(), ch, CollectOptions{
		Since:          now.AddDate(0, 0, -30),
		Matcher:        query.NewMatcher("private endpoints"),
		IncludeReplies: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// r1 (2d), m1 (3d), r3 (5d, reply in an old thread). Deleted, system and
	// out-of-window messages are excluded.
	got := ids(res.Messages)
	want := []string{"r1", "m1", "r3"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	m1 := res.Messages[1]
	if m1.Type != "message" || m1.ThreadID != "m1" || m1.Author != "Alice" || m1.ReplyCount == nil || *m1.ReplyCount != 2 ||
		m1.Text != "Should we use Private Endpoints for ACR?" {
		t.Fatalf("unexpected root message %+v", m1)
	}
	if r1 := res.Messages[0]; r1.Type != "reply" || r1.ThreadID != "m1" {
		t.Fatalf("unexpected reply %+v", r1)
	}

	// Paging should stop at the first page with no activity since --since
	// (m5/m6), so the final page (m7) is never requested.
	msgReqs := 0
	for _, r := range srv.Requests() {
		if strings.Contains(r, "/messages") {
			msgReqs++
		}
	}
	if msgReqs != 4 {
		t.Fatalf("expected 4 message page requests, got %d: %v", msgReqs, srv.Requests())
	}
}

func TestCollectNoRepliesAndMaxThreads(t *testing.T) {
	svc, _ := newService(t)
	ch := ChannelRef{TeamID: "team-1", ChannelID: "19:pe@thread.tacv2"}

	res, err := svc.Collect(context.Background(), ch, CollectOptions{Matcher: query.NewMatcher("private endpoints")})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Messages); len(got) != 5 || got[0] != "m1" { // m1, m4, m5, m6, m7
		t.Fatalf("no-replies got %v", got)
	}

	res, err = svc.Collect(context.Background(), ch, CollectOptions{MaxThreads: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.ThreadsScanned != 2 {
		t.Fatalf("expected truncation after 2 threads, got %+v", res)
	}
}

func TestThread(t *testing.T) {
	svc, _ := newService(t)
	ch := ChannelRef{TeamID: "team-1", ChannelID: "19:pe@thread.tacv2"}
	msgs, err := svc.Thread(context.Background(), ch, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(msgs); len(got) != 3 || got[0] != "m1" || got[1] != "r1" || got[2] != "r2" {
		t.Fatalf("thread got %v", got)
	}
}
