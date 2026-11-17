package handler

import (
	"errors"
	"net/http"

	"github.com/vtmocanu/uzi/api/internal/forgesvc"
	"github.com/vtmocanu/uzi/api/internal/httpx"
	"github.com/vtmocanu/uzi/api/internal/issuetracker"
)

// writeIssueWriteNotOffered answers the typed refusal the linked-repo write guard
// returns (PRD #4173 M4; after M5 only the writes no unit has lifted) as a 409 instead of a 502/500,
// and reports whether it did. Callers that claimed something before the write have
// already released it by the time they call this.
func writeIssueWriteNotOffered(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, forgesvc.ErrIssueWriteNotOffered) {
		return false
	}
	httpx.Error(w, http.StatusConflict, "this issue write is not offered on a repo linked to an issue tracker")
	return true
}

// writeLinkedMoveError answers a failed card move or State write on a repo linked
// to an issue tracker (PRD #4173 M5) and reports whether it did. A column or State
// the driver refused before any request is a 409, and a moved-away issue is a 404
// (D10); in both the cache is unchanged, so the card snaps back. Any other error
// is left to the caller's 502.
func writeLinkedMoveError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, issuetracker.ErrStateNotAllowed):
		httpx.Error(w, http.StatusConflict, "the target issue tracker State is not allowed for this move")
	case forgesvc.ColumnMoveRefused(err):
		httpx.Error(w, http.StatusConflict, "map this column to an issue tracker State first")
	case forgesvc.LinkedIssueGone(err):
		httpx.Error(w, http.StatusNotFound, "the issue is no longer in the linked project")
	default:
		return false
	}
	return true
}
