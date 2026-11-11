package forgesvc

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/store"
)

// PRD #4173 M4: the two MR-watch lanes stay disjoint and a linked repo's issue runs
// move to the board-free lane. An unlinked repo's runs match exactly as before.
func TestMRWatchLanesForLinkedAndUnlinkedReposLiveDB(t *testing.T) {
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
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}

	user, forgeConn, itConn := uuid.New(), uuid.New(), uuid.New()
	linkedRepo, plainRepo := uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`, user, fmt.Sprintf("lanes-%s@e2e", user))
	exec(`INSERT INTO forge_connections (id, user_id, forge_type, base_url, bot_username, bot_forge_user_id, token_ciphertext)
	      VALUES ($1, $2, 'gitlab', $3, 'bot', 1, $4)`, forgeConn, user, "https://"+uuid.NewString()+".forge.e2e", []byte{1})
	exec(`INSERT INTO issue_tracker_connections (id, user_id, kind, base_url, account_id, account_login, token_ciphertext)
	      VALUES ($1, $2, 'youtrack', $3, '1-1', 'bot', $4)`, itConn, user, "https://"+uuid.NewString()+".yt.e2e", []byte{1})
	exec(`INSERT INTO repos (id, connection_id, forge_project_id, path_with_namespace, web_url, default_branch, enabled,
	        issue_tracker_connection_id, issue_tracker_project_id, issue_tracker_project_short, eligibility_tag_id, eligibility_tag_name, issue_tracker_linked_at)
	      VALUES ($1, $2, 1, $3, 'https://forge.e2e/g/l', 'main', true, $4, '0-1', 'UZI', '5-1', 'uzi', now())`,
		linkedRepo, forgeConn, "g/linked-"+linkedRepo.String(), itConn)
	exec(`INSERT INTO repos (id, connection_id, forge_project_id, path_with_namespace, web_url, default_branch, enabled)
	      VALUES ($1, $2, 2, $3, 'https://forge.e2e/g/p', 'main', true)`, plainRepo, forgeConn, "g/plain-"+plainRepo.String())

	// Each repo gets the same two runs: a completed issue run with an opened MR (the
	// board lane's candidate on an unlinked repo) and a prompt run (the board-free
	// lane's, always).
	runs := map[uuid.UUID]map[string]uuid.UUID{}
	for _, repo := range []uuid.UUID{linkedRepo, plainRepo} {
		runs[repo] = map[string]uuid.UUID{"issue": uuid.New(), "prompt": uuid.New()}
		exec(`INSERT INTO issues (repo_id, forge_issue_iid, title, state, labels, web_url, forge_updated_at, synced_at)
		      VALUES ($1, 7, 't', 'opened', '["Human Review"]', 'https://forge.e2e/i/7', now(), now())`, repo)
		ins := `INSERT INTO runs (id, user_id, repo_id, kind, issue_iid, issue_title, issue_description, branch, mr_iid, mr_state, status)
		        VALUES ($1, $2, $3, $4, $5, 't', 'd', $6, $7, 'opened', 'completed')`
		exec(ins, runs[repo]["issue"], user, repo, "issue", 7, "agent/issue-7-"+repo.String(), 13)
		exec(ins, runs[repo]["prompt"], user, repo, "prompt", nil, "uzi/prompt-"+repo.String(), 21)
	}

	boardLane := func(repo uuid.UUID) map[uuid.UUID]bool {
		t.Helper()
		rows, err := q.ListMRWatchCandidates(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		got := map[uuid.UUID]bool{}
		for _, r := range rows {
			got[r.ID] = true
		}
		return got
	}
	freeLane := func(repo uuid.UUID) map[uuid.UUID]bool {
		t.Helper()
		rows, err := q.ListBoardFreeMRStateWatchCandidates(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		got := map[uuid.UUID]bool{}
		for _, r := range rows {
			got[r.ID] = true
		}
		return got
	}

	t.Run("unlinked repo: lanes exactly as before", func(t *testing.T) {
		b, f := boardLane(plainRepo), freeLane(plainRepo)
		r := runs[plainRepo]
		if len(b) != 1 || !b[r["issue"]] {
			t.Errorf("board lane = %v, want only the issue run", b)
		}
		if len(f) != 1 || !f[r["prompt"]] {
			t.Errorf("board-free lane = %v, want the prompt run only", f)
		}
	})

	t.Run("linked repo: the issue run moves to the board-free lane", func(t *testing.T) {
		b, f := boardLane(linkedRepo), freeLane(linkedRepo)
		r := runs[linkedRepo]
		if len(b) != 0 {
			t.Errorf("board lane = %v, want none for a linked repo", b)
		}
		if len(f) != 2 || !f[r["issue"]] || !f[r["prompt"]] {
			t.Errorf("board-free lane = %v, want both runs", f)
		}
		for _, id := range r {
			if b[id] && f[id] {
				t.Errorf("run %v is in both lanes", id)
			}
		}
	})
}
