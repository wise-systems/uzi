package issuetracker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// SetIssueState implements forge.IssueTracker. It writes the explicit state
// value Options names for the requested state and never guesses one; with that
// value unset it returns ErrStateMappingUnset. The value is checked against the
// project's State bundle first (checkTargetState), before the issue is read.
func (y *youTrack) SetIssueState(ctx context.Context, projectID, issueIID int64, state forge.IssueState) error {
	if _, err := y.requireProject(projectID); err != nil {
		return err
	}
	var value string
	var closing bool
	switch state {
	case forge.StateOpened:
		value = y.openState
	case forge.StateClosed:
		value, closing = y.closedState, true
	default:
		return y.fail("youtrack: state must be opened or closed")
	}
	if value == "" {
		return y.red.sentinel(ErrStateMappingUnset, 0, "no value configured for "+string(state))
	}
	if err := y.checkTargetState(ctx, value, closing); err != nil {
		return err
	}
	w, err := y.resolveIssue(ctx, projectID, issueIID)
	if err != nil {
		return err
	}
	return y.writeState(ctx, w, value)
}

// MoveToColumn moves the issue to a board column by writing the State value the
// connection maps to it. An empty column is the implicit Open column and writes
// the open state. A column with no mapped State is ErrColumnNotMapped, and
// neither case sends a request before the check. The target is then checked
// against the project's State bundle and the issue against the bound project, as
// SetIssueState does.
func (y *youTrack) MoveToColumn(ctx context.Context, projectID, issueIID int64, column string) error {
	return y.moveToColumn(ctx, projectID, issueIID, column)
}

func (y *youTrack) moveToColumn(ctx context.Context, projectID, issueIID int64, column string) error {
	if _, err := y.requireProject(projectID); err != nil {
		return err
	}
	var value string
	if column == "" {
		if y.openState == "" {
			return y.red.sentinel(ErrStateMappingUnset, 0, "no value configured for the open column")
		}
		value = y.openState
	} else {
		v, ok := y.columnStates[column]
		if !ok || v == "" {
			return y.red.sentinel(ErrColumnNotMapped, 0, "")
		}
		value = v
	}
	if err := y.checkTargetState(ctx, value, false); err != nil {
		return err
	}
	w, err := y.resolveIssue(ctx, projectID, issueIID)
	if err != nil {
		return err
	}
	return y.writeState(ctx, w, value)
}

// writeState sets the State field of issue w to the value named value.
func (y *youTrack) writeState(ctx context.Context, w ytIssue, value string) error {
	_, _, err := y.do(ctx, ytCall{
		method: http.MethodPost,
		path:   []string{"issues", w.ID},
		query:  url.Values{"fields": {"id"}},
		body: map[string]any{"customFields": []any{map[string]any{
			"name":  y.stateField,
			"$type": "StateIssueCustomField",
			"value": map[string]string{"name": value},
		}}},
		limit: objectBytes,
		prov:  forge.ResponseSuccess,
	}, nil)
	return err
}

// checkTargetState reads the bound project's State bundle (the ListStates code
// path) and refuses, with ErrStateNotAllowed, a target that the bundle lacks, or
// that is not closable when closing, or not openable otherwise. It runs before
// every State write, so a bundle edited after the mapping was saved is caught.
// The error quotes the name through quoteName, which bounds it.
func (y *youTrack) checkTargetState(ctx context.Context, name string, closing bool) error {
	states, err := y.ListStates(ctx, y.project.ID)
	if err != nil {
		return err
	}
	for _, s := range states {
		if s.Name != name {
			continue
		}
		if closing && ClosableState(s) || !closing && OpenableState(s) {
			return nil
		}
		return y.red.sentinel(ErrStateNotAllowed, 0, "the state is not allowed in this role: "+quoteName(name))
	}
	return y.red.sentinel(ErrStateNotAllowed, 0, "the project's state bundle has no such state: "+quoteName(name))
}
