package issuetrackertest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// Subject is one implementation of forge.IssueTracker under contract test,
// with the seams the suite needs to put state in it that the interface cannot.
type Subject struct {
	IssueTracker forge.IssueTracker
	// ProjectID is passed as the projectID argument of every call.
	ProjectID int64
	// SeedHumanComment adds a comment by username on issue iid, bypassing the
	// IssueTracker under test.
	SeedHumanComment func(t *testing.T, iid int64, username, body string)
	// SeedLabelEvent records that username added ("add") or removed ("remove")
	// label on issue iid, bypassing the IssueTracker under test.
	SeedLabelEvent func(t *testing.T, iid int64, action, label, username string)
	// SeedDeletedComment adds a comment that the issue tracker reports as
	// deleted. Nil means the implementation has no such comment.
	SeedDeletedComment func(t *testing.T, iid int64, username, body string)
	// MoveIssue moves issue iid to another project. Nil means issues do not
	// move on this implementation.
	MoveIssue func(t *testing.T, iid int64)
}

// uniqueSeq makes every name the suite creates unique within the process, so
// a subject whose backing store is shared never sees a collision.
var uniqueSeq atomic.Int64

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, uniqueSeq.Add(1))
}

// RunContract runs the shared behaviour of every issue tracker implementation.
// newSubject is called once per case, so a case starts from the state its
// constructor gives. Assertions look for the data the case created and never
// count everything the subject holds, because a subject may hold other data.
func RunContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	cases := []struct {
		name string
		run  func(t *testing.T, c *contract)
	}{
		{"EnsureLabelsIsIdempotent", caseEnsureLabels},
		{"CreateIssueRoundTrips", caseCreateIssue},
		{"ListIssuesFilters", caseListIssues},
		{"UpdateLabelsAndDescription", caseUpdate},
		{"SetIssueStateClosesAndReopens", caseSetState},
		{"CommentsOldestFirst", caseComments},
		{"LabelEventsOldestFirst", caseLabelEvents},
		{"MissingIssueIsNotFound", caseMissing},
		{"MovedIssueIsRefused", caseMoved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSubject(t)
			tc.run(t, &contract{Subject: s, ctx: t.Context()})
		})
	}
}

type contract struct {
	Subject
	ctx context.Context
}

func (c *contract) ensure(t *testing.T, names ...string) {
	t.Helper()
	labels := make([]forge.Label, 0, len(names))
	for _, n := range names {
		labels = append(labels, forge.Label{Name: n, Color: "#428bca"})
	}
	if err := c.IssueTracker.EnsureLabels(c.ctx, c.ProjectID, labels); err != nil {
		t.Fatalf("EnsureLabels(%v): %v", names, err)
	}
}

func (c *contract) create(t *testing.T, description string, labels ...string) forge.Issue {
	t.Helper()
	issue, err := c.IssueTracker.CreateIssue(c.ctx, c.ProjectID, unique("contract issue"), description, labels)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.IID <= 0 {
		t.Fatalf("CreateIssue returned IID %d", issue.IID)
	}
	return issue
}

func (c *contract) get(t *testing.T, iid int64) forge.Issue {
	t.Helper()
	issue, err := c.IssueTracker.GetIssue(c.ctx, c.ProjectID, iid)
	if err != nil {
		t.Fatalf("GetIssue(%d): %v", iid, err)
	}
	return issue
}

// listIIDs returns the sorted IIDs ListIssues finds for opts.
func (c *contract) listIIDs(t *testing.T, opts forge.ListIssuesOptions) []int64 {
	t.Helper()
	issues, err := c.IssueTracker.ListIssues(c.ctx, c.ProjectID, opts)
	if err != nil {
		t.Fatalf("ListIssues(%+v): %v", opts, err)
	}
	iids := make([]int64, 0, len(issues))
	for _, i := range issues {
		iids = append(iids, i.IID)
	}
	slices.Sort(iids)
	return iids
}

func wantIIDs(t *testing.T, got []int64, want ...int64) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ListIssues IIDs = %v, want %v", got, want)
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func caseEnsureLabels(t *testing.T, c *contract) {
	a, b := unique("contract-label"), unique("contract-label")
	c.ensure(t, a, b)
	c.ensure(t, a, b) // idempotent
	labels, err := c.IssueTracker.ListLabels(c.ctx, c.ProjectID)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	count := map[string]int{}
	for _, l := range labels {
		count[l.Name]++
	}
	for _, name := range []string{a, b} {
		if count[name] != 1 {
			t.Errorf("label %q listed %d times, want once", name, count[name])
		}
	}
}

func caseCreateIssue(t *testing.T, c *contract) {
	l1, l2 := unique("contract-label"), unique("contract-label")
	c.ensure(t, l1, l2)
	created := c.create(t, "the description", l1, l2)
	got := c.get(t, created.IID)
	if got.IID != created.IID {
		t.Errorf("IID = %d, want %d", got.IID, created.IID)
	}
	if got.Title != created.Title || got.Title == "" {
		t.Errorf("Title = %q, created as %q", got.Title, created.Title)
	}
	if got.Description != "the description" {
		t.Errorf("Description = %q", got.Description)
	}
	if !sameSet(got.Labels, []string{l1, l2}) {
		t.Errorf("Labels = %v, want %v and %v", got.Labels, l1, l2)
	}
	if got.State != string(forge.StateOpened) {
		t.Errorf("State = %q, want opened", got.State)
	}
	if got.WebURL == "" || got.WebURL != created.WebURL {
		t.Errorf("WebURL = %q, created as %q", got.WebURL, created.WebURL)
	}
	if got.Assignees == nil {
		t.Error("Assignees is nil, want an empty slice")
	}
}

func caseListIssues(t *testing.T, c *contract) {
	l1, l2 := unique("contract-label"), unique("contract-label")
	c.ensure(t, l1, l2)
	both := c.create(t, "both", l1, l2)
	onlyOne := c.create(t, "one", l1)
	other := c.create(t, "two", l2)

	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1, l2}}), both.IID)
	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1}}), both.IID, onlyOne.IID)
	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l2}}), both.IID, other.IID)

	if err := c.IssueTracker.SetIssueState(c.ctx, c.ProjectID, onlyOne.IID, forge.StateClosed); err != nil {
		t.Fatalf("SetIssueState: %v", err)
	}
	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1}, State: forge.StateOpened}), both.IID)
	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1}, State: forge.StateClosed}), onlyOne.IID)
	wantIIDs(t, c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1}, State: forge.StateAll}), both.IID, onlyOne.IID)

	limited := c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{l1}, Limit: 1})
	if len(limited) != 1 {
		t.Fatalf("Limit 1 returned %d issues", len(limited))
	}
	if limited[0] != both.IID && limited[0] != onlyOne.IID {
		t.Errorf("Limit 1 returned %d, want one of the matches %d or %d", limited[0], both.IID, onlyOne.IID)
	}
}

func caseUpdate(t *testing.T, c *contract) {
	l1, l2 := unique("contract-label"), unique("contract-label")
	c.ensure(t, l1, l2)
	issue := c.create(t, "before", l1)
	if err := c.IssueTracker.UpdateIssueLabels(c.ctx, c.ProjectID, issue.IID, []string{l2}, []string{l1}); err != nil {
		t.Fatalf("UpdateIssueLabels: %v", err)
	}
	if err := c.IssueTracker.UpdateIssueDescription(c.ctx, c.ProjectID, issue.IID, "after"); err != nil {
		t.Fatalf("UpdateIssueDescription: %v", err)
	}
	got := c.get(t, issue.IID)
	if !sameSet(got.Labels, []string{l2}) {
		t.Errorf("Labels = %v, want only %v", got.Labels, l2)
	}
	if got.Description != "after" {
		t.Errorf("Description = %q, want after", got.Description)
	}
}

func caseSetState(t *testing.T, c *contract) {
	label := unique("contract-label")
	c.ensure(t, label)
	issue := c.create(t, "state", label)
	inList := func(state forge.IssueState) bool {
		return slices.Contains(c.listIIDs(t, forge.ListIssuesOptions{Labels: []string{label}, State: state}), issue.IID)
	}
	if err := c.IssueTracker.SetIssueState(c.ctx, c.ProjectID, issue.IID, forge.StateClosed); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := c.get(t, issue.IID).State; got != string(forge.StateClosed) {
		t.Errorf("State after close = %q", got)
	}
	if !inList(forge.StateClosed) || inList(forge.StateOpened) {
		t.Error("ListIssues state filters disagree with a closed issue")
	}
	if err := c.IssueTracker.SetIssueState(c.ctx, c.ProjectID, issue.IID, forge.StateOpened); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := c.get(t, issue.IID).State; got != string(forge.StateOpened) {
		t.Errorf("State after reopen = %q", got)
	}
	if inList(forge.StateClosed) || !inList(forge.StateOpened) {
		t.Error("ListIssues state filters disagree with a reopened issue")
	}
}

func caseComments(t *testing.T, c *contract) {
	issue := c.create(t, "comments")
	human, note := unique("human comment"), unique("bot note")
	c.SeedHumanComment(t, issue.IID, "alice", human)
	created, err := c.IssueTracker.CreateIssueNote(c.ctx, c.ProjectID, issue.IID, note)
	if err != nil {
		t.Fatalf("CreateIssueNote: %v", err)
	}
	if created.ID == 0 || created.Body != note {
		t.Errorf("CreateIssueNote = %+v, want a non-zero ID and body %q", created, note)
	}
	comments, err := c.IssueTracker.ListIssueComments(c.ctx, c.ProjectID, issue.IID)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	var humanAt, noteAt = -1, -1
	for i, cm := range comments {
		switch cm.Body {
		case human:
			humanAt = i
			if cm.AuthorUsername != "alice" {
				t.Errorf("human comment author = %q, want alice", cm.AuthorUsername)
			}
		case note:
			noteAt = i
		}
	}
	if c.SeedDeletedComment != nil {
		gone := unique("contract deleted")
		c.SeedDeletedComment(t, issue.IID, "carol", gone)
		again, err := c.IssueTracker.ListIssueComments(c.ctx, c.ProjectID, issue.IID)
		if err != nil {
			t.Fatalf("ListIssueComments: %v", err)
		}
		for _, cm := range again {
			if cm.Body == gone {
				t.Errorf("a deleted comment was listed: %+v", cm)
			}
		}
	}
	if humanAt < 0 || noteAt < 0 {
		t.Fatalf("comments %+v are missing the seeded comment or the note", comments)
	}
	if humanAt > noteAt {
		t.Errorf("human comment at %d after the note at %d, want oldest first", humanAt, noteAt)
	}
}

func caseLabelEvents(t *testing.T, c *contract) {
	label := unique("contract-label")
	c.ensure(t, label)
	issue := c.create(t, "events")
	c.SeedLabelEvent(t, issue.IID, "add", label, "alice")
	c.SeedLabelEvent(t, issue.IID, "remove", label, "bob")
	events, err := c.IssueTracker.ListIssueLabelEvents(c.ctx, c.ProjectID, issue.IID)
	if err != nil {
		t.Fatalf("ListIssueLabelEvents: %v", err)
	}
	var mine []forge.LabelEvent
	for _, e := range events {
		if e.LabelName == label {
			mine = append(mine, e)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("events for %q = %+v, want an add then a remove", label, mine)
	}
	if mine[1].CreatedAt.Before(mine[0].CreatedAt) {
		t.Errorf("events out of order: %v before %v", mine[1].CreatedAt, mine[0].CreatedAt)
	}
	if mine[0].Action != "add" || mine[0].Username != "alice" {
		t.Errorf("first event = %+v, want add by alice", mine[0])
	}
	if mine[1].Action != "remove" || mine[1].Username != "bob" {
		t.Errorf("second event = %+v, want remove by bob", mine[1])
	}
}

func caseMissing(t *testing.T, c *contract) {
	_, err := c.IssueTracker.GetIssue(c.ctx, c.ProjectID, 987654321)
	var notFound *forge.IssueNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetIssue of a missing IID = %v, want *forge.IssueNotFoundError", err)
	}
}

func caseMoved(t *testing.T, c *contract) {
	if c.MoveIssue == nil {
		t.Skip("issues do not move on this implementation")
	}
	issue := c.create(t, "will move")
	c.MoveIssue(t, issue.IID)
	it, pid, iid := c.IssueTracker, c.ProjectID, issue.IID
	calls := map[string]func() error{
		"GetIssue": func() error { _, err := it.GetIssue(c.ctx, pid, iid); return err },
		"UpdateIssueLabels": func() error {
			return it.UpdateIssueLabels(c.ctx, pid, iid, []string{unique("contract-label")}, nil)
		},
		"UpdateIssueDescription": func() error { return it.UpdateIssueDescription(c.ctx, pid, iid, "x") },
		"SetIssueState":          func() error { return it.SetIssueState(c.ctx, pid, iid, forge.StateClosed) },
		"ListIssueLabelEvents":   func() error { _, err := it.ListIssueLabelEvents(c.ctx, pid, iid); return err },
		"ListIssueComments":      func() error { _, err := it.ListIssueComments(c.ctx, pid, iid); return err },
		"CreateIssueNote":        func() error { _, err := it.CreateIssueNote(c.ctx, pid, iid, "x"); return err },
	}
	for name, call := range calls {
		var notFound *forge.IssueNotFoundError
		if err := call(); !errors.As(err, &notFound) {
			t.Errorf("%s on a moved issue = %v, want *forge.IssueNotFoundError", name, err)
		}
	}
}
