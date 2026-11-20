package issuetrackersvc

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/issuetracker"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

const wantStateMachineReason = "project " + youtracktest.ProjectShortName +
	" uses a state-machine workflow on its State field; uzi needs a plain State field. " +
	"Link a project with a plain State field, or remove the state-machine rule from the State field in YouTrack."

func TestStateMachineRefusalText(t *testing.T) {
	if got := StateMachineRefusal("ABC"); got != "project ABC uses a state-machine workflow on its State field; uzi needs a plain State field. "+
		"Link a project with a plain State field, or remove the state-machine rule from the State field in YouTrack." {
		t.Fatalf("StateMachineRefusal = %q", got)
	}
}

func TestLinkRefusesAProjectWhoseStateFieldIsAStateMachine(t *testing.T) {
	h := newYTHarness(t)
	h.srv.SeedIssue("a card", "body", youtracktest.EligibilityTagID)
	h.srv.SetStateMachine(youtracktest.ProjectID, true)
	repo := h.q.addRepo(h.user, true)

	h.srv.ResetRequests()
	e := wantKind(t, h.link(repo, youtracktest.EligibilityTagID), KindConflict)
	if e.Message != wantStateMachineReason || !strings.Contains(e.Message, "plain State field") {
		t.Errorf("message = %q, want %q", e.Message, wantStateMachineReason)
	}
	if h.q.repo(repo).linkedConn != uuid.Nil {
		t.Error("the repo was linked")
	}
	for _, r := range h.srv.Requests() {
		if r.Method != http.MethodGet {
			t.Errorf("request %s %s is not a read", r.Method, r.Path)
		}
	}
}

func TestLinkAPlainStateProjectStillLinksAndMapsControl(t *testing.T) {
	h := newYTHarness(t)
	h.srv.SeedIssue("a card", "body", youtracktest.EligibilityTagID)
	repo := linkedMappingRepo(t, h)
	if _, err := h.svc.SetStateMapping(context.Background(), h.user, repo, validMapping()); err != nil {
		t.Fatal(err)
	}
	if !h.q.repo(repo).hasStates {
		t.Error("the mapping was not saved")
	}
}

func TestStateMappingRefusesAStateMachineProjectAndSavesNothing(t *testing.T) {
	h := newYTHarness(t)
	h.srv.SeedIssue("a card", "body", youtracktest.EligibilityTagID)
	repo := linkedMappingRepo(t, h)
	ctx := context.Background()
	h.srv.SetStateMachine(youtracktest.ProjectID, true)

	_, err := h.svc.GetStateMapping(ctx, h.user, repo)
	if e := wantKind(t, err, KindConflict); e.Message != wantStateMachineReason {
		t.Errorf("GetStateMapping message = %q", e.Message)
	}
	_, err = h.svc.SetStateMapping(ctx, h.user, repo, validMapping())
	if e := wantKind(t, err, KindConflict); e.Message != wantStateMachineReason {
		t.Errorf("SetStateMapping message = %q", e.Message)
	}
	if h.q.repo(repo).hasStates {
		t.Error("a refused SetStateMapping saved a mapping")
	}
}

func TestStateFieldCheckFailureIsABadGatewayNamingTheProject(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newYTHarness(t)
			h.srv.SeedIssue("a card", "body", youtracktest.EligibilityTagID)
			repo := linkedMappingRepo(t, h)
			unlinked := h.q.addRepo(h.user, true)
			h.srv.FailIssueListing(status)

			e := wantKind(t, h.link(unlinked, youtracktest.EligibilityTagID), KindBadGateway)
			if !strings.Contains(e.Message, "could not check project "+youtracktest.ProjectShortName+"'s State field") {
				t.Errorf("link message = %q", e.Message)
			}
			if h.q.repo(unlinked).linkedConn != uuid.Nil {
				t.Error("the repo was linked")
			}
			_, err := h.svc.GetStateMapping(context.Background(), h.user, repo)
			if e := wantKind(t, err, KindBadGateway); !strings.Contains(e.Message, youtracktest.ProjectShortName) {
				t.Errorf("GetStateMapping message = %q", e.Message)
			}
			_, err = h.svc.SetStateMapping(context.Background(), h.user, repo, validMapping())
			_ = wantKind(t, err, KindBadGateway)
		})
	}
}

// The fakeDriver path: a driver that reports the sentinel is a Conflict, any other error a 502.
func TestCheckStateFieldMapsDriverErrors(t *testing.T) {
	ref := issuetracker.ProjectRef{ID: "0-1", ShortName: "ABC"}
	d := &fakeDriver{stateFieldErr: issuetracker.ErrStateMachineField}
	if e := wantKind(t, checkStateField(context.Background(), d, ref), KindConflict); e.Message != StateMachineRefusal("ABC") {
		t.Errorf("message = %q", e.Message)
	}
	if e := wantKind(t, checkStateField(context.Background(), &fakeDriver{stateFieldErr: errors.New("x")}, ref), KindBadGateway); !strings.HasPrefix(e.Message, "could not check project ABC's State field: ") {
		t.Errorf("message = %q", e.Message)
	}
	if err := checkStateField(context.Background(), &fakeDriver{}, ref); err != nil {
		t.Errorf("plain State field: %v", err)
	}
}
