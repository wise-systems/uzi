package poller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/forge/forgetest"
	"github.com/vtmocanu/uzi/api/internal/forgesvc"
	"github.com/vtmocanu/uzi/api/internal/issuetracker"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
	"github.com/vtmocanu/uzi/api/internal/issuetrackersvc"
	"github.com/vtmocanu/uzi/api/internal/secretbox"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// PRD #4173 M4 through the poller: a repo linked to an issue tracker still runs every
// code-host step when the issue side is unavailable, never runs autopilot, and records
// an issue run's MR state through the board-free lane with no cached card.

// trustFake lets the real YouTrack driver (which takes no TLS option by design) trust
// the youtracktest certificate for this test only.
func trustFake(t *testing.T, srv *youtracktest.Server) {
	t.Helper()
	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("http.DefaultTransport is not an *http.Transport")
	}
	saved := def.TLSClientConfig
	def.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(func() { def.TLSClientConfig = saved })
}

type refusingDrivers struct{ calls int }

func (r *refusingDrivers) DriverFor(context.Context, uuid.UUID, uuid.UUID, issuetracker.Binding, string, string, string) (issuetracker.Driver, error) {
	r.calls++
	return nil, errors.New("base URL is not in the allowed issue tracker list")
}

type oneRepoStore struct {
	q    *store.Queries
	repo uuid.UUID
}

func (r oneRepoStore) ListEnabledReposWithConnections(ctx context.Context) ([]store.ListEnabledReposWithConnectionsRow, error) {
	rows, err := r.q.ListEnabledReposWithConnections(ctx)
	var out []store.ListEnabledReposWithConnectionsRow
	for _, row := range rows {
		if row.ID == r.repo {
			out = append(out, row)
		}
	}
	return out, err
}

type linkedEnv struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	q       *store.Queries
	svc     *forgesvc.Service
	rec     *forgetest.Recorder
	engine  *Engine
	starter *apRuns
	user    uuid.UUID
	repo    uuid.UUID
	run7    uuid.UUID
}

// newLinkedEnv seeds one enabled repo linked to a real issuetrackersvc connection to
// srv, with an autopilot-labelled card cached (issue 5) and a completed issue run
// (issue 7, MR 13 recorded opened, forge reports it closed) with NO cached card.
func newLinkedEnv(t *testing.T, srv *youtracktest.Server, drivers forgesvc.IssueTrackerDrivers) *linkedEnv {
	t.Helper()
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
	env := &linkedEnv{ctx: ctx, pool: pool, q: q, svc: svc, rec: rec, user: uuid.New(), repo: uuid.New(), run7: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,email,password_hash,autopilot_enabled) VALUES($1,$2,'x',true)", env.user, fmt.Sprintf("%s@linked.test", env.user))
	conn := uuid.New()
	exec("INSERT INTO forge_connections(id,user_id,forge_type,base_url,bot_username,bot_forge_user_id,human_username,token_ciphertext) VALUES($1,$2,'gitlab',$3,'bot',1,'alice',$4)", conn, env.user, "https://"+uuid.NewString()+".forge.test", sealed)
	exec("INSERT INTO user_secrets(user_id,kind,label,is_default,ciphertext,sealed_with) VALUES($1,'anthropic_token','default',true,$2,'master')", env.user, []byte{2})

	itConn := uuid.New()
	if srv != nil {
		itSvc := issuetrackersvc.New(q, box, func(u string) bool { return u == srv.URL }, issuetracker.New)
		c, err := itSvc.Connect(ctx, env.user, "youtrack", srv.URL, srv.Token)
		if err != nil {
			t.Fatal(err)
		}
		itConn = c.ID
		if drivers == nil {
			drivers = itSvc
		}
	} else {
		exec(`INSERT INTO issue_tracker_connections (id, user_id, kind, base_url, account_id, account_login, token_ciphertext)
		      VALUES ($1, $2, 'youtrack', $3, '1-1', 'bot', $4)`, itConn, env.user, "https://"+uuid.NewString()+".yt.test", []byte{1})
	}
	svc.SetIssueTrackerDrivers(drivers)
	exec(`INSERT INTO repos(id,connection_id,forge_project_id,path_with_namespace,web_url,default_branch,enabled,
	        issue_tracker_connection_id, issue_tracker_project_id, issue_tracker_project_short, eligibility_tag_id, eligibility_tag_name, issue_tracker_linked_at)
	      VALUES($1,$2,42,$3,'https://forge.test/g/r','main',true,$4,$5,$6,$7,$8,now())`,
		env.repo, conn, env.repo.String(), itConn, youtracktest.ProjectID, youtracktest.ProjectShortName, youtracktest.EligibilityTagID, youtracktest.EligibilityTagName)
	exec("INSERT INTO board_columns(repo_id,label_name,position) VALUES($1,'In Progress',0),($1,'Human Review',1)", env.repo)
	// A cached card carrying the autopilot label: an unlinked repo would run the detector on it.
	exec(`INSERT INTO issues (repo_id, forge_issue_iid, title, state, labels, web_url, forge_updated_at, synced_at)
	      VALUES ($1, 5, 'autopilot', 'opened', '["uzi","autopilot"]', 'https://forge.test/i/5', now(), now())`, env.repo)
	exec("INSERT INTO runs(id,user_id,repo_id,kind,issue_iid,issue_title,issue_description,branch,mr_iid,mr_state,status,created_at) VALUES($1,$2,$3,'issue',7,'t','d',$4,13,'opened','completed','2020-01-01')",
		env.run7, env.user, env.repo, "agent/issue-7-"+env.repo.String())

	// A merged run that declared a moved PRD: SyncPRDLinkPatches has a candidate and
	// reads the issue side for it, which would panic on a nil tracker.
	exec("INSERT INTO runs(id,user_id,repo_id,kind,issue_iid,issue_title,issue_description,branch,mr_iid,mr_state,prd_done_path,status,created_at) VALUES($1,$2,$3,'issue',8,'t','Implements prds/72-x.md end to end.',$4,31,'merged','prds/done/72-x.md','completed','2020-01-01')",
		uuid.New(), env.user, env.repo, "agent/issue-8-"+env.repo.String())

	env.engine = New(svc, oneRepoStore{q, env.repo}, time.Minute, 3)
	env.engine.SetPipelineWatch(time.Hour, 5)
	env.starter = &apRuns{}
	env.engine.SetAutopilot(NewAutopilot(q, env.starter, nil))
	return env
}

// poll runs one tick with the default logger captured and fails if the repo's sync
// panicked (guardPanic would then silently skip every later code-host step).
func (e *linkedEnv) poll(t *testing.T) {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)
	e.engine.PollOnce(e.ctx)
	if strings.Contains(logs.String(), "repo sync panicked") {
		t.Fatalf("the repo sync panicked: %s", logs.String())
	}
}

func (e *linkedEnv) assertCodeHostOnly(t *testing.T) {
	t.Helper()
	var state *string
	if err := e.pool.QueryRow(e.ctx, `SELECT mr_state FROM runs WHERE id = $1`, e.run7).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == nil || *state != forge.MRStateClosed {
		t.Fatalf("issue run's mr_state = %v, want closed: the board-free lane must record a linked repo's issue run with no cached card", state)
	}
	calls := e.rec.Calls()
	if !containsPrefix(calls, "GetMergeRequest(") {
		t.Fatalf("the code-host steps did not run: %v", calls)
	}
	// The last code-host step of the pipeline watch ran: a panic in an earlier issue-side
	// step would have skipped it.
	if !containsPrefix(calls, "LatestPipeline(") {
		t.Fatalf("SyncPipelines did not run after the issue-side steps: %v", calls)
	}
	for _, c := range calls {
		for _, issueSide := range []string{"ListIssues(", "GetIssue(", "ListIssueLabelEvents(", "EnsureLabels(", "UpdateIssueLabels(", "ListLabels("} {
			if strings.HasPrefix(c, issueSide) {
				t.Errorf("the forge saw an issue-side call %q for a linked repo", c)
			}
		}
	}
	if len(e.starter.calls) != 0 {
		t.Fatalf("autopilot ran on a linked repo: %+v", e.starter.calls)
	}
}

func containsPrefix(ss []string, prefix string) bool {
	for _, s := range ss {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestLinkedRepoCodeHostStepsRunWhenTheTrackerIsUnreachableLiveDB(t *testing.T) {
	srv := youtracktest.NewServer(t)
	trustFake(t, srv)
	env := newLinkedEnv(t, srv, nil)
	srv.Close() // the tracker is gone from here on

	env.poll(t)
	env.assertCodeHostOnly(t)
}

func TestLinkedRepoCodeHostStepsRunWhenTheResolverRefusesLiveDB(t *testing.T) {
	drivers := &refusingDrivers{}
	env := newLinkedEnv(t, nil, drivers)

	env.poll(t)
	if drivers.calls == 0 {
		t.Fatal("the resolver was never asked for the linked repo's issue side")
	}
	env.assertCodeHostOnly(t)
}

// With no driver source wired a linked repo fails closed on its issue side and never
// falls back to the forge, yet its code-host steps still run.
func TestLinkedRepoWithNoDriverSourceNeverFallsBackToTheForgeLiveDB(t *testing.T) {
	env := newLinkedEnv(t, nil, nil)
	env.svc.SetIssueTrackerDrivers(nil)

	env.poll(t)
	env.assertCodeHostOnly(t)
}

type syncOutcome struct {
	ok    bool
	class forge.ErrorClass
}

// recordOutcomes wires the engine's issue-sync health observer and returns what it saw.
func recordOutcomes(e *linkedEnv) *[]syncOutcome {
	var seen []syncOutcome
	e.engine.SetSyncAttempt(func(uuid.UUID) func(bool, forge.ErrorClass) {
		return func(ok bool, class forge.ErrorClass) { seen = append(seen, syncOutcome{ok, class}) }
	})
	return &seen
}

// A linked repo reports its issue sync healthy only when the linked sync succeeded, and
// still runs every code-host step when it did not.
func TestLinkedRepoHealthFollowsTheLinkedIssueSyncLiveDB(t *testing.T) {
	t.Run("healthy issue tracker reports ok", func(t *testing.T) {
		srv := youtracktest.NewServer(t)
		trustFake(t, srv)
		env := newLinkedEnv(t, srv, nil)
		seen := recordOutcomes(env)
		env.poll(t)
		if len(*seen) != 1 || !(*seen)[0].ok {
			t.Fatalf("outcomes = %+v, want one healthy report", *seen)
		}
	})
	t.Run("a rejected issue tracker token reports class other, not a forge auth failure", func(t *testing.T) {
		srv := youtracktest.NewServer(t)
		trustFake(t, srv)
		env := newLinkedEnv(t, srv, nil)
		seen := recordOutcomes(env)
		// The stored token no longer matches the one the fake accepts: YouTrack answers 401.
		srv.Token += "-rotated"
		env.poll(t)
		if len(*seen) != 1 || (*seen)[0].ok || (*seen)[0].class != forge.ErrorClassOther {
			t.Fatalf("outcomes = %+v, want one unhealthy report of class %q (the forge connection is not at fault)", *seen, forge.ErrorClassOther)
		}
		env.assertCodeHostOnly(t)
	})
	t.Run("an unreachable issue tracker reports unhealthy, code-host steps still run", func(t *testing.T) {
		srv := youtracktest.NewServer(t)
		trustFake(t, srv)
		env := newLinkedEnv(t, srv, nil)
		seen := recordOutcomes(env)
		srv.Close()
		env.poll(t)
		if len(*seen) != 1 || (*seen)[0].ok || (*seen)[0].class != forge.ErrorClassOther {
			t.Fatalf("outcomes = %+v, want one unhealthy report of class %q", *seen, forge.ErrorClassOther)
		}
		env.assertCodeHostOnly(t)
	})
	t.Run("a refused resolver reports unhealthy, code-host steps still run", func(t *testing.T) {
		env := newLinkedEnv(t, nil, &refusingDrivers{})
		seen := recordOutcomes(env)
		env.poll(t)
		if len(*seen) != 1 || (*seen)[0].ok || (*seen)[0].class != forge.ErrorClassOther {
			t.Fatalf("outcomes = %+v, want one unhealthy report of class %q", *seen, forge.ErrorClassOther)
		}
		env.assertCodeHostOnly(t)
	})
}

// The poller's linked tick caches the eligibility tag's issues with their keys.
func TestLinkedRepoPollCachesTaggedIssuesLiveDB(t *testing.T) {
	srv := youtracktest.NewServer(t)
	trustFake(t, srv)
	env := newLinkedEnv(t, srv, nil)
	n := srv.SeedIssue("tagged", "body", youtracktest.EligibilityTagID)
	env.poll(t)
	var key *string
	if err := env.pool.QueryRow(env.ctx, `SELECT issue_key FROM issues WHERE repo_id = $1 AND forge_issue_iid = $2`, env.repo, n).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%s-%d", youtracktest.ProjectShortName, n); key == nil || *key != want {
		t.Fatalf("issue_key = %v, want %s", key, want)
	}
}
