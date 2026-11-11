package issuetracker

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// TestLabelGuardOnEveryGuardedMethod sends a brace, and a name one byte over
// the length limit, through every method that takes label names.
func TestLabelGuardOnEveryGuardedMethod(t *testing.T) {
	y, log := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, "[]") })
	ctx := context.Background()
	for _, name := range []string{"a}b", strings.Repeat("a", maxLabelNameLen+1)} {
		calls := map[string]func() error{
			"EnsureLabels": func() error { return y.EnsureLabels(ctx, 1, []forge.Label{{Name: name}}) },
			"ListIssues": func() error {
				_, err := y.ListIssues(ctx, 1, forge.ListIssuesOptions{Labels: []string{name}})
				return err
			},
			"CreateIssue":              func() error { _, err := y.CreateIssue(ctx, 1, "t", "d", []string{name}); return err },
			"UpdateIssueLabels add":    func() error { return y.UpdateIssueLabels(ctx, 1, 1, []string{name}, nil) },
			"UpdateIssueLabels remove": func() error { return y.UpdateIssueLabels(ctx, 1, 1, nil, []string{name}) },
		}
		for method, call := range calls {
			if err := call(); !errors.Is(err, ErrInvalidLabelName) {
				t.Errorf("%s(len %d) = %v, want ErrInvalidLabelName", method, len(name), err)
			}
		}
	}
	if got := log.all(); len(got) != 0 {
		t.Errorf("guarded calls sent %v", got)
	}
}

func TestUpdateIssueLabelsSkipsAbsentTagRemoval(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("t", "", "keep", "drop")
	srv.ResetRequests()
	if err := y.UpdateIssueLabels(context.Background(), 1, n, nil, []string{"never-there", "drop"}); err != nil {
		t.Fatal(err)
	}
	var deletes int
	for _, r := range srv.Requests() {
		if r.Method == http.MethodDelete {
			deletes++
		}
	}
	if deletes != 1 {
		t.Errorf("sent %d DELETE requests, want 1 (only the tag on the issue)", deletes)
	}
	if got := srv.IssueTags(n); !slices.Equal(got, []string{"keep"}) {
		t.Errorf("tags = %v, want [keep]", got)
	}
}

func TestTagIDsForTakesTheFirstTagOfADuplicatedName(t *testing.T) {
	y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[{"id":"5-1","name":"dup"},{"id":"5-2","name":"dup"}]`)
	})
	ids, err := y.tagIDsFor(context.Background(), []string{"dup"})
	if err != nil || ids["dup"] != "5-1" {
		t.Fatalf("tagIDsFor = %v, %v; want the first tag, 5-1", ids, err)
	}
	labels, err := y.ListLabels(context.Background(), 1)
	if err != nil || len(labels) != 1 || labels[0].Name != "dup" {
		t.Fatalf("ListLabels = %+v, %v; want one dup", labels, err)
	}
}

func TestCreateIssueAttachesADuplicateLabelOnce(t *testing.T) {
	y, srv := newFakeDriver(t)
	srv.ResetRequests()
	if _, err := y.CreateIssue(context.Background(), 1, "t", "d", []string{"x", "x"}); err != nil {
		t.Fatal(err)
	}
	var attaches int
	for _, r := range srv.Requests() {
		if r.Method == http.MethodPost && strings.HasPrefix(r.Path, "/api/issues/") && strings.HasSuffix(r.Path, "/tags") {
			attaches++
		}
	}
	if attaches != 1 {
		t.Errorf("attached %d times, want once", attaches)
	}
}

// TestMalformedTagIDsNeverReachAPath feeds ids that would alter a path.
func TestMalformedTagIDsNeverReachAPath(t *testing.T) {
	const bad = ".."
	t.Run("on the issue", func(t *testing.T) {
		y, log := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) {
			writeRaw(w, rawIssue("2-1", "UZI-1", 1, "0-1", `{"id":"`+bad+`","name":"x"}`))
		})
		if err := y.UpdateIssueLabels(context.Background(), 1, 1, nil, []string{"x"}); err == nil {
			t.Error("UpdateIssueLabels accepted a malformed tag id")
		}
		if got := log.writes(); len(got) != 0 {
			t.Errorf("sent %v", got)
		}
	})
	t.Run("in the tag list", func(t *testing.T) {
		y, log := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				writeRaw(w, `[{"id":"`+bad+`","name":"x"}]`)
				return
			}
			writeRaw(w, rawIssue("2-1", "UZI-1", 1, "0-1", ""))
		})
		ctx := context.Background()
		if _, err := y.ListLabels(ctx, 1); err == nil {
			t.Error("ListLabels accepted a malformed tag id")
		}
		if err := y.UpdateIssueLabels(ctx, 1, 1, []string{"x"}, nil); err == nil {
			t.Error("UpdateIssueLabels attached a malformed tag id")
		}
		if got := log.writes(); len(got) != 0 {
			t.Errorf("sent %v", got)
		}
	})
	t.Run("in a created tag", func(t *testing.T) {
		y, log := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" && r.Method == http.MethodGet {
				writeRaw(w, `[]`)
				return
			}
			if r.URL.Path == "/api/tags" {
				writeRaw(w, `{"id":"`+bad+`","name":"x"}`)
				return
			}
			writeRaw(w, rawIssue("2-1", "UZI-1", 1, "0-1", ""))
		})
		if err := y.UpdateIssueLabels(context.Background(), 1, 1, []string{"x"}, nil); err == nil {
			t.Error("UpdateIssueLabels attached a malformed created tag id")
		}
		for _, w := range log.writes() {
			if strings.Contains(w, "/issues/") {
				t.Errorf("sent %v", w)
			}
		}
	})
}
