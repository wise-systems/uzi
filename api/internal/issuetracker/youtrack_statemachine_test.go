package issuetracker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

var mainRef = ProjectRef{ID: youtracktest.ProjectID, ShortName: youtracktest.ProjectShortName}

func TestCheckStateField(t *testing.T) {
	t.Run("a state-machine project is refused", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		srv.SetStateMachine(youtracktest.ProjectID, true)
		if err := y.CheckStateField(context.Background(), mainRef); !errors.Is(err, ErrStateMachineField) {
			t.Fatalf("CheckStateField = %v, want ErrStateMachineField", err)
		}
	})
	t.Run("a plain project passes", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		if err := y.CheckStateField(context.Background(), mainRef); err != nil {
			t.Fatalf("CheckStateField = %v, want nil", err)
		}
	})
	t.Run("an empty project passes", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SetStateMachine(youtracktest.OtherProjectID, true)
		other := ProjectRef{ID: youtracktest.OtherProjectID, ShortName: youtracktest.OtherProjectShortName}
		if err := y.CheckStateField(context.Background(), other); err != nil {
			t.Fatalf("CheckStateField = %v, want nil", err)
		}
	})
	t.Run("an issue of another project does not count and a page of them is an error", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		srv.SetStateMachine(youtracktest.ProjectID, true)
		// The ref names the main project's short name but another project's id.
		ref := ProjectRef{ID: youtracktest.OtherProjectID, ShortName: youtracktest.ProjectShortName}
		err := y.CheckStateField(context.Background(), ref)
		if err == nil || errors.Is(err, ErrStateMachineField) {
			t.Fatalf("CheckStateField = %v, want a plain error", err)
		}
	})
	t.Run("another custom field run by a state machine does not count", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		srv.SetExtraStateMachineField(youtracktest.ProjectID, "Workflow")
		if err := y.CheckStateField(context.Background(), mainRef); err != nil {
			t.Fatalf("CheckStateField = %v, want nil", err)
		}
	})
	t.Run("a malformed ref sends no request", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.ResetRequests()
		if err := y.CheckStateField(context.Background(), ProjectRef{ID: "0-1", ShortName: "a b"}); err == nil || errors.Is(err, ErrStateMachineField) {
			t.Fatalf("CheckStateField = %v, want a refusal", err)
		}
		if n := len(srv.Requests()); n != 0 {
			t.Errorf("%d requests sent", n)
		}
	})
	t.Run("the State field is the configured one", func(t *testing.T) {
		srv, y := newFakeDriverWithField(t, "Stage")
		srv.SeedIssue("a", "")
		srv.SetStateMachine(youtracktest.ProjectID, true)
		if err := y.CheckStateField(context.Background(), mainRef); !errors.Is(err, ErrStateMachineField) {
			t.Fatalf("CheckStateField = %v, want ErrStateMachineField", err)
		}
	})
}

func TestCheckStateFieldRequestShape(t *testing.T) {
	y, srv := newFakeDriver(t)
	srv.SeedIssue("a", "")
	srv.ResetRequests()
	if err := y.CheckStateField(context.Background(), mainRef); err != nil {
		t.Fatal(err)
	}
	rs := srv.Requests()
	if len(rs) != 1 || rs[0].Method != http.MethodGet || rs[0].Path != "/api/issues" {
		t.Fatalf("requests = %v, want one GET /api/issues", rs)
	}
	q, err := url.ParseQuery(rs[0].RawQuery)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.Get("query"), issueQuery(youtracktest.ProjectShortName, forge.ListIssuesOptions{}); got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if got := q["customFields"]; len(got) != 1 || got[0] != "State" {
		t.Errorf("customFields = %v, want [State]", got)
	}
	if q.Get("$skip") != "0" || q.Get("$top") == "" {
		t.Errorf("not the first page: %s", rs[0].RawQuery)
	}
}

func TestCheckStateFieldReadsOnlyOnePage(t *testing.T) {
	y, srv := newFakeDriver(t)
	for range pageSize + 5 {
		srv.SeedIssue("a", "")
	}
	srv.ResetRequests()
	if err := y.CheckStateField(context.Background(), mainRef); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestCheckStateFieldFailedReadIsNeverAPass(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		srv.FailIssueListing(status)
		err := y.CheckStateField(context.Background(), mainRef)
		if err == nil || errors.Is(err, ErrStateMachineField) {
			t.Errorf("status %d: CheckStateField = %v, want a non-nil error that is not ErrStateMachineField", status, err)
		}
	}
}

func TestStateWriteRefusesAStateMachineIssue(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(y *youTrack, n int64) error{
		"SetIssueState": func(y *youTrack, n int64) error {
			return y.SetIssueState(ctx, testProjectID, n, forge.StateClosed)
		},
		"MoveToColumn": func(y *youTrack, n int64) error {
			return y.MoveToColumn(ctx, testProjectID, n, forge.ColumnMove{Column: "In Progress"})
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			y, srv := columnDriver(t, nil)
			n := srv.SeedIssue("t", "")
			srv.SetStateMachine(youtracktest.ProjectID, true)
			srv.ResetRequests()
			if err := call(y, n); !errors.Is(err, ErrStateMachineField) {
				t.Fatalf("%s = %v, want ErrStateMachineField", name, err)
			}
			if w := writesOf(srv); len(w) != 0 {
				t.Errorf("writes sent: %v", w)
			}
		})
	}
}

func TestMoveToColumnOnAPlainProjectStillWrites(t *testing.T) {
	y, srv := columnDriver(t, nil)
	n := srv.SeedIssue("t", "")
	srv.ResetRequests()
	if err := y.MoveToColumn(context.Background(), testProjectID, n, forge.ColumnMove{Column: "In Progress"}); err != nil {
		t.Fatal(err)
	}
	if w := writesOf(srv); len(w) != 1 || !strings.HasPrefix(w[0], "POST /api/issues/") {
		t.Errorf("writes = %v, want one POST to the issue", w)
	}
	if got := srv.IssueState(n); got != "In Progress" {
		t.Errorf("state = %q", got)
	}
}

func TestAnotherStateMachineFieldDoesNotBlockTheStateWrite(t *testing.T) {
	y, srv := columnDriver(t, nil)
	n := srv.SeedIssue("t", "")
	srv.SetExtraStateMachineField(youtracktest.ProjectID, "Workflow")
	srv.ResetRequests()
	if err := y.MoveToColumn(context.Background(), testProjectID, n, forge.ColumnMove{Column: "In Progress"}); err != nil {
		t.Fatalf("MoveToColumn = %v, want nil", err)
	}
	if w := writesOf(srv); len(w) != 1 || !strings.HasPrefix(w[0], "POST /api/issues/") {
		t.Errorf("writes = %v, want one POST to the issue", w)
	}
}
