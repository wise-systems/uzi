package forgesvc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// PRD #4173 D10: only the linked sync writes issues.issue_key. The forge sync writes it
// NULL, and a label-only cache write leaves it alone.
func TestIssueKeyIsWrittenOnlyByTheLinkedSyncLiveDB(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("UZI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UZI_TEST_DATABASE_URL not set; run via e2e/run-store-it.sh for live-DB coverage")
	}
	if err := store.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	q := store.New(pool)

	userID, connID, repoID := uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`, []any{userID, fmt.Sprintf("issue-key-%s@e2e", userID)}},
		{`INSERT INTO forge_connections (id, user_id, forge_type, base_url, bot_username, bot_forge_user_id, token_ciphertext)
		  VALUES ($1, $2, 'gitlab', 'https://forge.e2e', 'bot', 1, $3)`, []any{connID, userID, []byte{0x1}}},
		{`INSERT INTO repos (id, connection_id, forge_project_id, path_with_namespace, web_url, enabled)
		  VALUES ($1, $2, 100, 'g/r', 'https://forge.e2e/g/r', true)`, []any{repoID, connID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	readKey := func(iid int64) pgtype.Text {
		t.Helper()
		row, err := q.GetIssueByIID(ctx, store.GetIssueByIIDParams{RepoID: repoID, ForgeIssueIid: iid})
		if err != nil {
			t.Fatalf("GetIssueByIID %d: %v", iid, err)
		}
		return row.IssueKey
	}

	// A keyed row survives a label-only write.
	if _, err := q.UpsertIssue(ctx, store.UpsertIssueParams{
		RepoID: repoID, ForgeIssueIid: 9, Title: "keyed", State: "opened", Labels: []byte(`[]`), AssigneeIds: []byte(`[]`),
		WebUrl: "https://x", ForgeUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		IssueKey: pgtype.Text{String: "ABC-9", Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertIssueLabels(ctx, store.UpsertIssueLabelsParams{
		RepoID: repoID, ForgeIssueIid: 9, Title: "keyed", State: "opened", Labels: []byte(`[]`), AddLabels: []byte(`["x"]`), RemoveLabels: []byte(`[]`),
		WebUrl: "https://x", ForgeUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readKey(9); !got.Valid || got.String != "ABC-9" {
		t.Fatalf("issue_key after a label-only write = %+v, want ABC-9", got)
	}

	// The forge full sync writes NULL, for a new row and for an existing one.
	f := &fakeForge{issues: []forge.Issue{issueAt(1, recordedMarkBase), issueAt(9, recordedMarkBase)}}
	if _, err := newTestService(q).FullSync(ctx, repoID, 100, f); err != nil {
		t.Fatal(err)
	}
	for _, iid := range []int64{1, 9} {
		if got := readKey(iid); got.Valid {
			t.Fatalf("forge FullSync wrote issue_key %q for issue %d, want NULL", got.String, iid)
		}
	}
}
