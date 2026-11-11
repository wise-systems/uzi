package issuetracker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

func TestIssueQuery(t *testing.T) {
	after := time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
	got := issueQuery("UZI", forge.ListIssuesOptions{
		Labels: []string{"a b", "c"}, State: forge.StateOpened, UpdatedAfter: &after,
	})
	want := "project: {UZI} tag: {a b} tag: {c} #Unresolved updated: 2026-02-28 .. * sort by: updated asc"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := issueQuery("UZI", forge.ListIssuesOptions{State: forge.StateClosed}); got != "project: {UZI} #Resolved sort by: updated asc" {
		t.Fatalf("got %q", got)
	}
}

func TestLabelNameGuard(t *testing.T) {
	y, _ := newFakeDriver(t)
	ctx := context.Background()
	for _, name := range []string{"", "  ", "a}b", "a{b", "a\nb", "a‮b"} {
		if err := y.EnsureLabels(ctx, 1, []forge.Label{{Name: name}}); !errors.Is(err, ErrInvalidLabelName) {
			t.Errorf("EnsureLabels(%q) = %v, want ErrInvalidLabelName", name, err)
		}
		if _, err := y.ListIssues(ctx, 1, forge.ListIssuesOptions{Labels: []string{name}}); !errors.Is(err, ErrInvalidLabelName) {
			t.Errorf("ListIssues(%q) = %v, want ErrInvalidLabelName", name, err)
		}
	}
}

func TestListIssuesUpdatedAfterAndLimit(t *testing.T) {
	y, srv := newFakeDriver(t)
	ctx := context.Background()
	first := srv.SeedIssue("one", "")
	second := srv.SeedIssue("two", "")
	all, err := y.ListIssues(ctx, 1, forge.ListIssuesOptions{})
	if err != nil || len(all) != 2 || all[0].IID != first {
		t.Fatalf("ListIssues = %+v, %v", all, err)
	}
	// The fake clock is one second per event; the day-granular query keeps
	// both, so only the driver's exact filter can drop the first.
	after := all[1].UpdatedAt
	got, err := y.ListIssues(ctx, 1, forge.ListIssuesOptions{UpdatedAfter: &after})
	if err != nil || len(got) != 1 || got[0].IID != second {
		t.Fatalf("UpdatedAfter = %+v, %v", got, err)
	}
	got, err = y.ListIssues(ctx, 1, forge.ListIssuesOptions{Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("Limit = %+v, %v", got, err)
	}
}

func TestIssueMapping(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("title", "body", "x", "y")
	got, err := y.GetIssue(context.Background(), 99, n)
	if err != nil {
		t.Fatal(err)
	}
	if got.IID != n || got.Title != "title" || got.Description != "body" || got.State != "opened" ||
		got.Author != youtracktestBot || got.AuthorForgeUserID != 0 || got.Assignees == nil || len(got.Assignees) != 0 ||
		got.WebURL != srv.URL+"/issue/UZI-1" || len(got.Labels) != 2 || got.UpdatedAt.IsZero() {
		t.Fatalf("mapped issue = %+v", got)
	}
}

func TestPagingWalksEveryPage(t *testing.T) {
	y, srv := newFakeDriver(t)
	for i := 0; i < pageSize+5; i++ {
		srv.SeedIssue("i", "")
	}
	got, err := y.ListIssues(context.Background(), 1, forge.ListIssuesOptions{})
	if err != nil || len(got) != pageSize+5 {
		t.Fatalf("got %d issues, %v", len(got), err)
	}
}

func TestStatesAndProjects(t *testing.T) {
	y, _ := newFakeDriver(t)
	ctx := context.Background()
	acct, err := y.VerifyToken(ctx)
	if err != nil || acct.Login != youtracktestBot {
		t.Fatalf("VerifyToken = %+v, %v", acct, err)
	}
	projects, err := y.ListProjects(ctx)
	if err != nil || len(projects) != 2 {
		t.Fatalf("ListProjects = %+v, %v", projects, err)
	}
	states, err := y.ListStates(ctx, "0-1")
	if err != nil || len(states) != 3 || states[0].Resolved || !states[1].Resolved || states[2].Name != "Won't fix" {
		t.Fatalf("ListStates = %+v, %v", states, err)
	}
	if _, err := y.ListStates(ctx, "../x"); err == nil {
		t.Fatal("ListStates accepted a malformed project id")
	}
}

func TestStateFieldOption(t *testing.T) {
	srv, y := newFakeDriverWithField(t, "Stage")
	_ = srv
	ctx := context.Background()
	if _, err := y.ListStates(ctx, "0-1"); err != nil {
		t.Fatal(err)
	}
}

func TestUnboundDriver(t *testing.T) {
	_, srv := newFakeDriver(t)
	opts := fakeOptions(srv)
	opts.Project = ProjectRef{}
	y, err := newYouTrackOver(opts, srv.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := y.ListLabels(ctx, 1); !errors.Is(err, ErrProjectNotBound) {
		t.Errorf("ListLabels = %v", err)
	}
	if _, err := y.GetIssue(ctx, 1, 1); !errors.Is(err, ErrProjectNotBound) {
		t.Errorf("GetIssue = %v", err)
	}
	if _, err := y.ListIssues(ctx, 1, forge.ListIssuesOptions{}); !errors.Is(err, ErrProjectNotBound) {
		t.Errorf("ListIssues = %v", err)
	}
	if _, err := y.ListProjects(ctx); err != nil {
		t.Errorf("ListProjects on an unbound driver: %v", err)
	}
}

func TestStateMappingUnset(t *testing.T) {
	_, srv := newFakeDriver(t)
	opts := fakeOptions(srv)
	opts.OpenState, opts.ClosedState = "", ""
	y, err := newYouTrackOver(opts, srv.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	n := srv.SeedIssue("t", "")
	for _, s := range []forge.IssueState{forge.StateOpened, forge.StateClosed} {
		if err := y.SetIssueState(context.Background(), 1, n, s); !errors.Is(err, ErrStateMappingUnset) {
			t.Errorf("SetIssueState(%s) = %v", s, err)
		}
	}
	y2, _ := newFakeDriver(t)
	if err := y2.SetIssueState(context.Background(), 1, 1, forge.StateAll); err == nil {
		t.Error("SetIssueState accepted StateAll")
	}
}

func TestConstructorValidation(t *testing.T) {
	good := Options{BaseURL: "https://yt.example.com", Token: credential(), Project: ProjectRef{ID: "0-1", ShortName: "UZI"}}
	mutate := func(f func(*Options)) Options { o := good; f(&o); return o }
	if _, err := New(KindYouTrack, good); err != nil {
		t.Fatalf("good options: %v", err)
	}
	if _, err := New(KindYouTrack, mutate(func(o *Options) { o.Project = ProjectRef{} })); err != nil {
		t.Fatalf("unbound options: %v", err)
	}
	bad := map[string]Options{
		"http":        mutate(func(o *Options) { o.BaseURL = "http://yt.example.com" }),
		"path":        mutate(func(o *Options) { o.BaseURL = "https://yt.example.com/youtrack" }),
		"userinfo":    mutate(func(o *Options) { o.BaseURL = "https://u:p@yt.example.com" }),
		"query":       mutate(func(o *Options) { o.BaseURL = "https://yt.example.com?x=1" }),
		"fragment":    mutate(func(o *Options) { o.BaseURL = "https://yt.example.com#x" }),
		"no host":     mutate(func(o *Options) { o.BaseURL = "https://" }),
		"empty":       mutate(func(o *Options) { o.BaseURL = "" }),
		"short token": mutate(func(o *Options) { o.Token = "short" }),
		"bad short":   mutate(func(o *Options) { o.Project.ShortName = "1X" }),
		"bad id":      mutate(func(o *Options) { o.Project.ID = "abc" }),
		"half bound":  mutate(func(o *Options) { o.Project.ID = "" }),
	}
	for name, o := range bad {
		if _, err := New(KindYouTrack, o); err == nil {
			t.Errorf("%s: constructor accepted %+v", name, o)
		}
	}
	if _, err := New(KindYouTrack, mutate(func(o *Options) { o.BaseURL = "https://yt.example.com/" })); err != nil {
		t.Errorf("trailing slash rejected: %v", err)
	}
}
