package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/forge/forgetest"
	"github.com/vtmocanu/uzi/api/internal/forgesvc"
	"github.com/vtmocanu/uzi/api/internal/privcheck"
)

// Recorded-call test (PRD 4173 M4): for a repo with no issue-tracker link,
// pin every forge call the repo-scoped handlers make: the first board open
// (default-column seed plus initial import), the Refresh sync, a board card
// move, a new board issue, and a repo enable (live guard, column seed, import
// after enable). The forge is reached
// through the real forgesvc builder seam and the real privcheck guard, so the
// calls are the production ones. The golden was captured before the
// tracker-link rerouting; regenerate only deliberately, with
// UZI_UPDATE_GOLDEN=1.

// rcRepoForge answers the issue and guard reads those handlers make.
type rcRepoForge struct{ forgetest.BaseFake }

func (*rcRepoForge) ListIssues(_ context.Context, _ int64, opts forge.ListIssuesOptions) ([]forge.Issue, error) {
	return []forge.Issue{{IID: 5, Title: "t", State: "opened", Labels: opts.Labels, UpdatedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)}}, nil
}
func (*rcRepoForge) UpdateIssueLabels(context.Context, int64, int64, []string, []string) error {
	return nil
}
func (*rcRepoForge) CreateIssue(_ context.Context, _ int64, title, _ string, labels []string) (forge.Issue, error) {
	return forge.Issue{IID: 6, Title: title, State: "opened", Labels: labels, UpdatedAt: time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)}, nil
}
func (*rcRepoForge) EnsureLabels(context.Context, int64, []forge.Label) error { return nil }
func (*rcRepoForge) VerifyToken(context.Context) (forge.BotIdentity, error) {
	return forge.BotIdentity{ForgeUserID: 9, Username: "bot"}, nil
}
func (*rcRepoForge) ProjectRole(context.Context, int64, int64) (forge.Role, bool, error) {
	return forge.RoleWrite, true, nil
}
func (*rcRepoForge) DefaultBranchProtection(context.Context, int64, string, int64) (forge.BranchProtection, error) {
	return forge.BranchProtection{Protected: true}, nil
}

func TestRecordedCallsRepoHandlersLiveDB(t *testing.T) {
	h, router, pool := cliLiveDB(t)
	rec := forgetest.NewRecorder(&rcRepoForge{})
	h.svc = forgesvc.NewWithForgeBuilder(h.q, h.box, time.Second, nil, func(forge.Type, string, string, time.Duration) (forge.Forge, error) {
		return rec, nil
	})
	h.pcheck = privcheck.NewService(h.q, h.svc)

	sealed, err := h.svc.EncryptToken("fixture")
	if err != nil {
		t.Fatal(err)
	}
	owner := cliSeedUser(t, pool, false)
	jwt := cliMintJWT(t, pool, owner)
	connID := uuid.New()
	cliMustExec(t, pool,
		`INSERT INTO forge_connections (id, user_id, forge_type, base_url, bot_username, bot_forge_user_id, token_ciphertext)
		 VALUES ($1, $2, 'gitlab', 'https://forge.e2e', 'bot', 9, $3)`, connID, owner, sealed)
	seedRepo := func(projectID int64, enabled bool) uuid.UUID {
		id := uuid.New()
		cliMustExec(t, pool,
			`INSERT INTO repos (id, connection_id, forge_project_id, path_with_namespace, web_url, default_branch, enabled)
			 VALUES ($1, $2, $3, $4, $5, 'main', $6)`,
			id, connID, projectID, fmt.Sprintf("g/rc-%d", projectID), fmt.Sprintf("https://forge.e2e/g/rc-%d", projectID), enabled)
		return id
	}
	// Project ids are distinct per fixture so the golden names which repo a call
	// belongs to.
	tracked := seedRepo(4173001, true) // enabled, no board columns yet
	toEnable := seedRepo(4173002, false)

	var lines []string
	step := func(name, method, path, body string, want int) {
		t.Helper()
		before := len(rec.Calls())
		w := cookieReq(t, router, method, path, jwt, body)
		if w.Code != want {
			t.Fatalf("%s: status = %d, want %d, body %s", name, w.Code, want, w.Body.String())
		}
		calls := rec.Calls()[before:]
		if len(calls) == 0 {
			t.Fatalf("%s: no forge call recorded", name)
		}
		lines = append(lines, "# "+name)
		lines = append(lines, calls...)
	}
	step("GetBoard, repo with no columns (board seed)", http.MethodGet, fmt.Sprintf("/api/repos/%s/board", tracked), "", http.StatusOK)
	step("SyncRepo (Refresh)", http.MethodPost, fmt.Sprintf("/api/repos/%s/sync", tracked), "", http.StatusOK)
	// Issue 5 was cached by the import above; drag it into a column.
	step("MoveIssue (board card move)", http.MethodPost, fmt.Sprintf("/api/repos/%s/issues/5/move", tracked), `{"to_column":"In Progress"}`, http.StatusOK)
	step("CreateIssue (board new issue)", http.MethodPost, fmt.Sprintf("/api/repos/%s/issues", tracked), `{"title":"new card","description":"body"}`, http.StatusCreated)
	step("SetRepoEnabled true (guard, column seed, importAfterEnable)", http.MethodPut, fmt.Sprintf("/api/repos/%s", toEnable), `{"enabled":true}`, http.StatusOK)

	forgetest.AssertGolden(t, "recorded_calls_handler_repo_handlers.golden", lines)
}
