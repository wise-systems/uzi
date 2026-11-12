package forgesvc

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// ErrLinkedSyncNotReady is returned by the issue sync dispatchers for a repo linked
// to an external issue tracker until the linked intake lands (PRD #4173 M4 intake).
// It is a typed refusal, not a forge failure: callers log it and carry on with
// every code-host step, and never map it to a forge error class.
var ErrLinkedSyncNotReady = errors.New("linked issue sync is not available yet")

// SyncIssues is the one entry point for a full issue sync into the cache. tr is the
// repo's issue side as IssueTrackerForRepo returned it: a linked repo's tracker runs
// the linked sync, which this milestone leaves as ErrLinkedSyncNotReady so the intake
// milestone replaces a single function; anything else is a forge driver and runs
// FullSync. The choice is derived from tr, so it cannot disagree with it.
func (s *Service) SyncIssues(ctx context.Context, repoID uuid.UUID, forgeProjectID int64, tr forge.IssueTracker) (Marks, error) {
	if isLinkedTracker(tr) {
		return Marks{}, s.syncLinkedIssues(ctx, repoID)
	}
	return s.FullSync(ctx, repoID, forgeProjectID, tr)
}

// SyncIssuesIncremental is SyncIssues for the poller's between-reconcile tick.
// For a linked repo the caller's marks come back unchanged with the typed error.
func (s *Service) SyncIssuesIncremental(ctx context.Context, repoID uuid.UUID, forgeProjectID int64, tr forge.IssueTracker, m Marks) (Marks, error) {
	if isLinkedTracker(tr) {
		return m, s.syncLinkedIssues(ctx, repoID)
	}
	return s.IncrementalSync(ctx, repoID, forgeProjectID, tr, m)
}

// syncLinkedIssues is the linked repo's sync body; replaced by the intake milestone.
func (s *Service) syncLinkedIssues(context.Context, uuid.UUID) error {
	return ErrLinkedSyncNotReady
}
