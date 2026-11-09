package forge

import (
	"reflect"
	"sort"
	"testing"
)

// issueTrackerMethods pins the issue-side half of the Forge contract (PRD #8 D1).
// Moving a method across the IssueTracker/CodeHost seam must change this list.
var issueTrackerMethods = []string{
	"CreateIssue",
	"CreateIssueNote",
	"EnsureLabels",
	"GetIssue",
	"ListIssueComments",
	"ListIssueLabelEvents",
	"ListIssues",
	"ListLabels",
	"SetIssueState",
	"UpdateIssueDescription",
	"UpdateIssueLabels",
}

func methodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// firstDiff returns the first name present in exactly one of two sorted lists.
func firstDiff(a, b []string) (string, bool) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			i++
			j++
		case a[i] < b[j]:
			return a[i], true
		default:
			return b[j], true
		}
	}
	if i < len(a) {
		return a[i], true
	}
	if j < len(b) {
		return b[j], true
	}
	return "", false
}

func TestInterfaceSplitPinned(t *testing.T) {
	tracker := methodNames(reflect.TypeOf((*IssueTracker)(nil)).Elem())
	host := methodNames(reflect.TypeOf((*CodeHost)(nil)).Elem())
	all := methodNames(reflect.TypeOf((*Forge)(nil)).Elem())

	inTracker := make(map[string]bool, len(tracker))
	for _, n := range tracker {
		inTracker[n] = true
	}
	for _, n := range host {
		if inTracker[n] {
			t.Fatalf("method %s is on both IssueTracker and CodeHost", n)
		}
	}

	union := append(append([]string{}, tracker...), host...)
	sort.Strings(union)
	if n, ok := firstDiff(union, all); ok {
		t.Fatalf("IssueTracker+CodeHost differs from Forge at method %s", n)
	}

	want := append([]string{}, issueTrackerMethods...)
	sort.Strings(want)
	if n, ok := firstDiff(tracker, want); ok {
		t.Fatalf("IssueTracker differs from the pinned issue-side list at method %s", n)
	}
}

func TestIssueTrackerIsNotCodeHost(t *testing.T) {
	tracker := reflect.TypeOf((*IssueTracker)(nil)).Elem()
	host := reflect.TypeOf((*CodeHost)(nil)).Elem()
	if tracker.Implements(host) {
		t.Fatal("IssueTracker satisfies CodeHost; the halves must not overlap")
	}
	if host.Implements(tracker) {
		t.Fatal("CodeHost satisfies IssueTracker; the halves must not overlap")
	}
}
