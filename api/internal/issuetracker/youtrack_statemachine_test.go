package issuetracker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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
	t.Run("the sample requests only the State custom field, so another state-machine field is never read", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		srv.SeedIssue("a", "")
		srv.SetExtraStateMachineField(youtracktest.ProjectID, "Workflow")
		srv.ResetRequests()
		if err := y.CheckStateField(context.Background(), mainRef); err != nil {
			t.Fatalf("CheckStateField = %v, want nil", err)
		}
		rs := srv.Requests()
		if len(rs) != 1 {
			t.Fatalf("requests = %v, want one", rs)
		}
		q, err := url.ParseQuery(rs[0].RawQuery)
		if err != nil {
			t.Fatal(err)
		}
		if got := q["customFields"]; len(got) != 1 || got[0] != "State" {
			t.Errorf("customFields = %v, want [State]", got)
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

var unreadableShapes = []youtracktest.StateFieldShape{youtracktest.StateFieldAbsent, youtracktest.StateFieldUntyped}

// stateWriteCalls are the two State writes, each run against issue n.
func stateWriteCalls() map[string]func(y *youTrack, n int64) error {
	ctx := context.Background()
	return map[string]func(y *youTrack, n int64) error{
		"SetIssueState": func(y *youTrack, n int64) error {
			return y.SetIssueState(ctx, testProjectID, n, forge.StateClosed)
		},
		"MoveToColumn": func(y *youTrack, n int64) error {
			return y.MoveToColumn(ctx, testProjectID, n, forge.ColumnMove{Column: "In Progress"})
		},
	}
}

func TestStateWriteRefusesAnUnreadableStateField(t *testing.T) {
	for _, shape := range unreadableShapes {
		for name, call := range stateWriteCalls() {
			t.Run(string(shape)+"/"+name, func(t *testing.T) {
				y, srv := columnDriver(t, nil)
				n := srv.SeedIssue("t", "")
				before := srv.IssueState(n)
				srv.SeedStateFieldShape(n, shape)
				srv.ResetRequests()
				err := call(y, n)
				if !errors.Is(err, ErrStateFieldUnreadable) || errors.Is(err, ErrStateMachineField) {
					t.Fatalf("%s = %v, want ErrStateFieldUnreadable only", name, err)
				}
				if want := "UZI-" + strconv.FormatInt(n, 10); !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
				if w := writesOf(srv); len(w) != 0 {
					t.Errorf("writes sent: %v", w)
				}
				if got := srv.IssueState(n); got != before {
					t.Errorf("state = %q, want %q unchanged", got, before)
				}
			})
		}
	}
}

// The State-write resolve refuses a malformed idReadable (checkIssue) before it
// classifies the State field, so the empty and over-long id-readable cases of
// the error's naming are reachable through CheckStateField, which reads no
// further than the sample.
func TestCheckStateFieldUnreadableNamesTheIDWhenIDReadableIsEmpty(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("t", "")
	srv.SeedStateFieldShape(n, youtracktest.StateFieldAbsent)
	srv.SeedIDReadable(n, "")
	err := y.CheckStateField(context.Background(), mainRef)
	if !errors.Is(err, ErrStateFieldUnreadable) {
		t.Fatalf("CheckStateField = %v, want ErrStateFieldUnreadable", err)
	}
	if !strings.Contains(err.Error(), "issue \"") || strings.Contains(err.Error(), "issue \"\"") {
		t.Errorf("error %q names no issue id", err)
	}
}

func TestCheckStateFieldUnreadableBoundsALongIDReadable(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("t", "")
	srv.SeedStateFieldShape(n, youtracktest.StateFieldUntyped)
	srv.SeedIDReadable(n, strings.Repeat("A", 500))
	err := y.CheckStateField(context.Background(), mainRef)
	if !errors.Is(err, ErrStateFieldUnreadable) {
		t.Fatalf("CheckStateField = %v, want ErrStateFieldUnreadable", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("A", maxQuotedNameLen+1)) {
		t.Errorf("error holds more than %d characters of the id: %q", maxQuotedNameLen, err)
	}
	if limit := len(ErrStateFieldUnreadable.Error()) + maxQuotedNameLen + 100; len(err.Error()) > limit {
		t.Errorf("error is %d bytes, want at most %d", len(err.Error()), limit)
	}
}

func TestStateWriteRefusesAMalformedIDReadableBeforeAnyWrite(t *testing.T) {
	y, srv := columnDriver(t, nil)
	n := srv.SeedIssue("t", "")
	srv.SeedStateFieldShape(n, youtracktest.StateFieldAbsent)
	srv.SeedIDReadable(n, "")
	srv.ResetRequests()
	err := y.SetIssueState(context.Background(), testProjectID, n, forge.StateClosed)
	if err == nil {
		t.Fatal("SetIssueState = nil, want a refusal")
	}
	if w := writesOf(srv); len(w) != 0 {
		t.Errorf("writes sent: %v", w)
	}
}

func TestSetIssueStateOnAPlainStateFieldWrites(t *testing.T) {
	y, srv := columnDriver(t, nil)
	n := srv.SeedIssue("t", "")
	srv.ResetRequests()
	if err := y.SetIssueState(context.Background(), testProjectID, n, forge.StateClosed); err != nil {
		t.Fatal(err)
	}
	if w := writesOf(srv); len(w) != 1 || !strings.HasPrefix(w[0], "POST /api/issues/") {
		t.Errorf("writes = %v, want one POST to the issue", w)
	}
	if got := srv.IssueState(n); got == "" || got == "Open" {
		t.Errorf("state = %q, want the closed value", got)
	}
}

func TestCheckStateFieldUnreadable(t *testing.T) {
	ctx := context.Background()
	for _, shape := range unreadableShapes {
		t.Run(string(shape)+" alone", func(t *testing.T) {
			y, srv := newFakeDriver(t)
			n := srv.SeedIssue("a", "")
			srv.SeedStateFieldShape(n, shape)
			err := y.CheckStateField(ctx, mainRef)
			if !errors.Is(err, ErrStateFieldUnreadable) || errors.Is(err, ErrStateMachineField) {
				t.Fatalf("CheckStateField = %v, want ErrStateFieldUnreadable only", err)
			}
			if want := "UZI-" + strconv.FormatInt(n, 10); !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		})
		t.Run(string(shape)+" among plain issues", func(t *testing.T) {
			y, srv := newFakeDriver(t)
			srv.SeedIssue("a", "")
			n := srv.SeedIssue("b", "")
			srv.SeedIssue("c", "")
			srv.SeedStateFieldShape(n, shape)
			err := y.CheckStateField(ctx, mainRef)
			if !errors.Is(err, ErrStateFieldUnreadable) || errors.Is(err, ErrStateMachineField) {
				t.Fatalf("CheckStateField = %v, want ErrStateFieldUnreadable only", err)
			}
			if want := "UZI-" + strconv.FormatInt(n, 10); !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		})
		t.Run(string(shape)+" with a state-machine issue, the state machine wins", func(t *testing.T) {
			y, srv := newFakeDriver(t)
			srv.SeedIssue("machine", "")
			n := srv.SeedIssue("shaped", "")
			srv.SetStateMachine(youtracktest.ProjectID, true)
			srv.SeedStateFieldShape(n, shape)
			if err := y.CheckStateField(ctx, mainRef); !errors.Is(err, ErrStateMachineField) || errors.Is(err, ErrStateFieldUnreadable) {
				t.Fatalf("CheckStateField = %v, want ErrStateMachineField only", err)
			}
		})
	}
	t.Run("the state-machine issue after the shaped one still wins", func(t *testing.T) {
		y, srv := newFakeDriver(t)
		n := srv.SeedIssue("shaped", "")
		srv.SeedIssue("machine", "")
		srv.SetStateMachine(youtracktest.ProjectID, true)
		srv.SeedStateFieldShape(n, youtracktest.StateFieldAbsent)
		if err := y.CheckStateField(ctx, mainRef); !errors.Is(err, ErrStateMachineField) {
			t.Fatalf("CheckStateField = %v, want ErrStateMachineField", err)
		}
	})
}

func TestCheckStateFieldAsksForIDReadable(t *testing.T) {
	y, srv := newFakeDriver(t)
	srv.SeedIssue("a", "")
	srv.ResetRequests()
	if err := y.CheckStateField(context.Background(), mainRef); err != nil {
		t.Fatal(err)
	}
	rs := srv.Requests()
	if len(rs) != 1 {
		t.Fatalf("requests = %v, want one", rs)
	}
	q, err := url.ParseQuery(rs[0].RawQuery)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(q.Get("fields"), ","), "idReadable") {
		t.Errorf("fields = %q, want idReadable among them", q.Get("fields"))
	}
}
