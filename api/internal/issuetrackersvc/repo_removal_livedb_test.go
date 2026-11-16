package issuetrackersvc

// Live-DB tests of the removal markers (PRD #4173 D13, migration 00427): a repo that was ever
// enabled stays unlinkable after it is removed and listed again, and a repo that was linked
// before its removal can never be enabled as a forge-issue repo again. They need
// UZI_TEST_DATABASE_URL like the rest of link_livedb_test.go.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vtmocanu/uzi/api/internal/provisionsvc"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// removeAndRelist deletes the repo the way the repo-removal route does and lists its project
// again the way the membership listing does, returning the new row.
func (h *liveHarness) removeAndRelist(repo uuid.UUID) store.Repo {
	h.t.Helper()
	ctx := context.Background()
	old := h.repoRow(repo)
	n, err := h.q.DeleteRepoForUser(ctx, store.DeleteRepoForUserParams{ID: repo, UserID: h.user})
	if err != nil {
		h.t.Fatal(err)
	}
	if n != 1 {
		h.t.Fatalf("DeleteRepoForUser affected %d rows, want 1", n)
	}
	r, err := h.q.UpsertRepo(ctx, store.UpsertRepoParams{
		ConnectionID: old.ConnectionID, ForgeProjectID: old.ForgeProjectID,
		PathWithNamespace: old.PathWithNamespace, WebUrl: old.WebUrl,
		DefaultBranch: pgtype.Text{String: "main", Valid: true},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	if r.ID == repo {
		h.t.Fatal("the re-listed repo kept the removed row's id")
	}
	return r
}

func TestUsedRepoRemovedAndRelistedIsNotLinkableLiveDB(t *testing.T) {
	h := newLiveHarness(t)
	repo := h.freshRepo()
	h.setEnabled(repo, true)
	h.setEnabled(repo, false)

	r := h.removeAndRelist(repo)
	if r.AwaitingFirstEnable {
		t.Fatal("a re-listed repo that had been enabled is awaiting its first enable again")
	}
	_ = wantKind(t, h.link(r.ID), KindConflict)
	if h.repoRow(r.ID).IssueTrackerConnectionID.Valid {
		t.Fatal("the repo was linked")
	}
}

func TestNeverEnabledRepoRemovedAndRelistedStaysLinkableLiveDB(t *testing.T) {
	h := newLiveHarness(t)
	repo := h.freshRepo()

	r := h.removeAndRelist(repo)
	if !r.AwaitingFirstEnable {
		t.Fatal("a re-listed repo that was never enabled lost its marker")
	}
	if err := h.link(r.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
}

func TestLinkedRepoRemovedAndRelistedCannotBeEnabledLiveDB(t *testing.T) {
	h := newLiveHarness(t)
	ctx := context.Background()
	repo := h.freshRepo()
	if err := h.link(repo); err != nil {
		t.Fatal(err)
	}
	h.setEnabled(repo, true)
	h.setEnabled(repo, false)

	r := h.removeAndRelist(repo)
	if r.AwaitingFirstEnable || r.IssueTrackerConnectionID.Valid {
		t.Fatalf("re-listed row awaiting = %v, linked = %v; want both false", r.AwaitingFirstEnable, r.IssueTrackerConnectionID.Valid)
	}

	_, err := h.provisioner(&labelRecorder{}, &gateGuard{}).EnableRepo(ctx, h.user, r.ID)
	var pe *provisionsvc.Error
	if !errors.As(err, &pe) || pe.Kind != provisionsvc.KindConflict {
		t.Fatalf("EnableRepo err = %v, want a *provisionsvc.Error of kind KindConflict", err)
	}

	_, err = h.pool.Exec(ctx, `UPDATE repos SET enabled = true WHERE id = $1`, r.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "UZ414" {
		t.Fatalf("raw enable err = %v, want SQLSTATE UZ414", err)
	}
	if h.repoRow(r.ID).Enabled {
		t.Fatal("the repo is enabled")
	}
}

func TestDeletingAForgeConnectionWithUsedReposLiveDB(t *testing.T) {
	h := newLiveHarness(t)
	ctx := context.Background()
	repo := h.freshRepo()
	h.setEnabled(repo, true)
	h.setEnabled(repo, false)
	project := h.repoRow(repo).ForgeProjectID

	h.exec(`DELETE FROM forge_connections WHERE id = $1`, h.forgeConn)

	var n int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM repos WHERE id = $1`, repo).Scan(&n); err != nil || n != 0 {
		t.Fatalf("repo rows after the connection delete = %d, err %v", n, err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM repo_removal_markers WHERE connection_id = $1 AND forge_project_id = $2`,
		h.forgeConn, project).Scan(&n); err != nil || n != 1 {
		t.Fatalf("markers left for the deleted connection = %d, err %v; want 1 (inert)", n, err)
	}
}
