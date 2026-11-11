package poller

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/forge/forgetest"
	"github.com/vtmocanu/uzi/api/internal/forgesvc"
	"github.com/vtmocanu/uzi/api/internal/secretbox"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// Recorded-call test (PRD 4173 M4): for an enabled repo with no issue-tracker
// link, pin every forge call a poller tick makes, for a full reconcile tick and
// then an incremental tick carrying the marks the first one produced. The
// optional detectors (autopilot, CI-autofix, MR-review watch, project reverse
// sync) are left unwired, as in the other poller live-DB tests; the pipeline
// watch is on so the CI-status sync's reads are part of the record. The golden
// was captured before the tracker-link rerouting; regenerate only deliberately,
// with UZI_UPDATE_GOLDEN=1.

type recordedPollForge struct{ forgetest.BaseFake }

var recordedPollAt = time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)

func (*recordedPollForge) ListIssues(_ context.Context, _ int64, opts forge.ListIssuesOptions) ([]forge.Issue, error) {
	return []forge.Issue{{IID: 5, Title: "t", State: "opened", Labels: opts.Labels, UpdatedAt: recordedPollAt}}, nil
}

func TestRecordedCallsPollerTickLiveDB(t *testing.T) {
	dsn := os.Getenv("UZI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UZI_TEST_DATABASE_URL unset; run e2e/run-store-it.sh")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := store.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := store.New(pool)
	box, err := secretbox.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	rec := forgetest.NewRecorder(&recordedPollForge{})
	svc := forgesvc.NewWithForgeBuilder(q, box, time.Second, nil, func(forge.Type, string, string, time.Duration) (forge.Forge, error) {
		return rec, nil
	})
	sealed, err := svc.EncryptToken("fixture")
	if err != nil {
		t.Fatal(err)
	}
	user, conn, repo := uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,email,password_hash) VALUES($1,$2,'x')", user, fmt.Sprintf("%s@recorded.test", user))
	exec("INSERT INTO forge_connections(id,user_id,forge_type,base_url,bot_username,bot_forge_user_id,token_ciphertext) VALUES($1,$2,'gitlab','https://forge.test','bot',1,$3)", conn, user, sealed)
	exec("INSERT INTO repos(id,connection_id,forge_project_id,path_with_namespace,web_url,default_branch,enabled) VALUES($1,$2,42,$3,'https://forge.test/g/r','main',true)", repo, conn, repo.String())

	// reconcileEvery 3: tick 1 is the first poll (full reconcile), tick 2 is
	// incremental.
	engine := New(svc, mergedRepoStore{q, repo}, time.Minute, 3)
	engine.SetPipelineWatch(time.Hour, 5)

	var lines []string
	for i, label := range []string{"tick 1 (full reconcile)", "tick 2 (incremental)"} {
		before := len(rec.Calls())
		engine.PollOnce(ctx)
		calls := rec.Calls()[before:]
		if len(calls) == 0 {
			t.Fatalf("tick %d made no forge call", i+1)
		}
		lines = append(lines, "# "+label)
		lines = append(lines, calls...)
	}
	forgetest.AssertGolden(t, "recorded_calls_poller_tick.golden", lines)
}
