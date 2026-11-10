# PRD ws-2: Issue tracking leaves the forge: `IssueTracker` and `CodeHost` interfaces, with GitLab, GitHub and Forgejo as the first issue trackers

**Issue:** [wise-systems/uzi#2](https://github.com/wise-systems/uzi/issues/2). Numbered `ws-2` so it never collides with an upstream `vtmocanu/uzi` PRD number (upstream already has `prds/done/2-forge-integration-kanban.md`).  
**Priority:** Medium  
**Status:** In progress, r3 2026-11-10: M2 delivered (every caller on its half; PR for wise-systems/uzi#2). r2 2026-11-10: M1 delivered (wise-systems/uzi#1). r1 2026-11-05. Source anchors checked against upstream `main` 6a76d09f7. Line numbers drift; cite by symbol.  
**Part of:** external issue trackers. This PRD is phase 1 and is complete on its own. M1 to M3 change no visible behaviour: GitLab, GitHub and Forgejo issues keep working as today, now through the issue-tracker layer. M4 and M5 add the new behaviours on GitLab, GitHub and Forgejo: the living comment with retried writes, automatic withdrawal, comments that reach the agent and `/uzi` commands. Phase 2 (connections, Settings → Issue trackers, board mapping) is a separate PRD that builds on it; Jira and Asana come later, one PRD each.  
**Related:**  
**Delivery:** one PRD, one PR per milestone, each merged before the next starts. No implementation or validation step touches `.github/workflows/**`.  
**Builds on:**

- **The forge layer** (`api/internal/forge`): one `Forge` interface, three drivers (GitLab, `adr/0065-forgejo-driver.md`, `adr/0238-github-driver.md`) and the shared fake `forgetest.BaseFake`.
- **The board column automation** (PRD #12, `api/internal/runlifecycle/`): moves a card by swapping labels when its run changes status.
- **Issue comment snapshots** (PRD #381): the issue's comments are saved and given to the agent when a run starts.

## Problem

uzi takes work only from forge issues, and teams that plan their work in an issue tracker (Jira, Asana) cannot use it without copying every task into the forge. An issue tracker cannot be plugged in today, because the forge layer does two jobs through one interface:

- **Code:** repos, merge requests, pipelines, branch protection, token and role checks.
- **Work items:** issues, labels, comments, issue state.

An issue tracker does only the second job, but every caller receives the whole `Forge`, and the privilege check calls three code-host methods on every run, fail-closed. Issues are identified by a forge issue number, and board columns are forge labels, so even the issue side assumes a forge.

Working from the issue is also limited on the forges themselves:

- A comment written on the issue while a run is live never reaches the agent; comments are read once, when the run starts.
- The run cannot be steered from the issue: approving a plan or answering a question needs uzi.
- uzi comments on the issue only in automatic flows (autopilot, CI autofix, MR rework), a new comment each time; a run started by hand says nothing on its issue, and a failed post is not retried.
- Removing the `uzi` label or closing the issue drops the card from the board but leaves the run going; an issue moved to another project is not detected.

This PRD separates the two jobs inside uzi, makes GitLab, GitHub and Forgejo the first implementations of the issue side, and closes these gaps on them. Adding an external issue tracker as a fourth forge would force it to pretend it has repos and merge requests, so the issue job has to come out of the forge first.

## Goal

1. `Forge` splits into two interfaces: `IssueTracker` (issues, labels, comments, state) and `CodeHost` (everything else). `Forge` stays as their union.
2. Every part of uzi that calls the forge receives only the half it needs; the privilege check receives only `CodeHost`.
3. Issue identity can carry a text key (`ABC-12`) beside the forge number, and moving a board card is a `IssueTracker` operation.
4. New comments on a run's issue reach the agent while the run is live.
5. The run's owner steers it from the issue with `/uzi` commands: approve, reject or revise a plan, answer a question, cancel the run.
6. uzi keeps one living status comment per run part, edited in place; new comments only for a plan to approve, a question and the final result. Every write lands, in order, never twice.
7. Removing the `uzi` label, closing the issue or moving it to another project takes the task out automatically.
8. Autopilot keeps working through the issue side.

## Out of scope

- Changing how the worker clones, pushes and opens the merge request: the code host is unchanged.
- Changing the board's columns or the `uzi` label semantics beyond what the decisions below name.

## What each uzi feature uses, today and after this PRD

- Bring labeled issues into uzi: today the `Forge`; after, `IssueTracker`.
- Show the board and move cards: today the `Forge` (labels); after, `IssueTracker` (`MoveToColumn`; the forge drivers still swap labels underneath).
- Write comments on the issue: today the `Forge`; after, `IssueTracker`.
- Close or reopen an issue: today the `Forge`; after, `IssueTracker`.
- Create an issue (board, findings, judge, review, self-improve, proposals): today the `Forge`; after, `IssueTracker`.
- Give the run the issue's title, body and comments: today the `Forge`; after, `IssueTracker`.
- Let the agent read an issue during a run through the worker API: today the `Forge`; after, `IssueTracker`, same routes.
- Pick issues for scheduled runs: today the `Forge`; after, `IssueTracker`.
- Privilege checks, claim and settle, pipeline sync, merge request watchers: today the `Forge`; after, `CodeHost`.
- Clone, push and open the MR (worker), CI pipelines and MR reviews: today the `Forge`; after, the `Forge` unchanged (`CodeHost`).

## Current state (verified on `main` 6a76d09f7, 2026-11-05)

- **One interface, 40 methods.** `forge.Forge` (`api/internal/forge/forge.go:746-995`). Eleven of them are issue-side: `ListLabels`, `EnsureLabels`, `ListIssues`, `GetIssue`, `CreateIssue`, `UpdateIssueLabels`, `UpdateIssueDescription`, `SetIssueState`, `ListIssueLabelEvents`, `ListIssueComments`, `CreateIssueNote` (`forge.go:762-819`; `UserExists` at `:801` sits between them but is an account check, not an issue method). The other 29 are code-host: repos and projects, merge requests and their comments and reviews, pipelines, jobs, checks, workflow runs and artifacts, branch and ref heads, ancestry, token, role and branch protection.
- **Four sites per interface change.** Each driver implements all 40 methods against one forge's API, and `forge.New` picks the driver from the connection's forge type (`api/internal/forge/forge.go:1000-1023`), so callers never know which forge a repo is on. Adding or changing a method therefore touches all four of these (`AGENTS.md:160`):
  - `gitLab` (`api/internal/forge/gitlab.go`): GitLab API v4, through `gitlab.com/gitlab-org/api/client-go`.
  - `github` (`api/internal/forge/github.go`): GitHub REST API, through `github.com/google/go-github`.
  - `forgejo` (`api/internal/forge/forgejo.go`): Forgejo API v1, through `code.gitea.io/sdk/gitea` and direct HTTP calls.
  - `forgetest.BaseFake` (`api/internal/forge/forgetest/basefake.go:47-52`): the shared stand-in the tests build their fakes on; a compile-time assertion makes it implement every method too.
- **References.** Occurrences of `forge.Forge` in production code by package (parameters, fields and comments, not distinct callers): `forgesvc` 26, `workersvc` 11, `schedsvc` 10, `privcheck` 8, `poller` 8, `handler` 6, `runlifecycle` 2, `issueinput` 2, `seed` 1.
- **Where the issue side is used today.** Every place in uzi's code, tests excluded, that calls one of the eleven issue-side methods through the `Forge` interface (47 in all; calls a driver makes inside itself are not counted), listed from the type-checked code (`golang.org/x/tools/go/packages`) and cross-checked with `gopls call_hierarchy`:
  - *Sync:* `forgesvc` `FullSync`, `IncrementalSync` and `reconcileFindingGroupMarkers` (`ListIssues`, `api/internal/forgesvc/service.go:787`, `:795`, `:816`, `:911`, `:915`, `:926`, `:448`).
  - *Board:* `AutoMove` and `SetIssueLabel` (`EnsureLabels`, `UpdateIssueLabels`, `forgesvc/service.go:534-542`, `:600-607`); column setup and label checks in the handlers (`EnsureLabels`, `ListLabels`, `api/internal/handler/board.go:366`, `:432`, `:760`, `handler/labels.go:36`, `:120`); close and reopen (`SetIssueState`, `forgesvc/close_reopen.go:29`, `:62`).
  - *Run creation:* the issue's text and comments (`GetIssue`, `ListIssueComments`, `api/internal/issueinput/projection.go:158`, `:169`), reached from `workersvc` `CreateRun`, the scheduler (`api/internal/schedsvc/scheduler.go:594`, `:806`), autopilot (`api/internal/poller/autopilot.go:209`) and the composite flow (`api/internal/workersvc/composite.go:212`); a task continuing an issue (`GetIssue` through the helper `forge.GetIssueBounded`, `forge/response_limit.go:114`, called from `workersvc/task.go:100`); semantic drift (`GetIssue`, `workersvc/semantic_drift.go:75`).
  - *The agent:* the worker reads issues through the API, `GET /api/worker/runs/{id}/forge/issues`, `…/issues/{iid}` and `…/issues/{iid}/label-events` (`ListIssues`, `GetIssue`, `ListIssueComments`, `ListIssueLabelEvents`, `handler/worker_forge.go:138`, `:183`, `:265`, `:310`).
  - *The issue page:* `GetIssueDetail` (`GetIssue`, `handler/issues.go:216`).
  - *Issues uzi creates:* the board's new issue (`handler/issues.go:65`), findings and finding groups, which also ensure their labels first (`EnsureLabels`, `handler/findings_file.go:163`, `:169`, `handler/findings_group_file.go:203`, `:227`), the judge's admin issue (`handler/judge_admin_issue.go:269`), review issues (`handler/review_issue_file.go:159`), the self-improve tracking issue (`schedsvc/self_improve.go:269`) and proposals (`workersvc/composite.go:150`, `workersvc/proposal_filing.go:175`), all through `CreateIssue`; the scheduler's proposal digest and self-improve lookup read issues (`ListIssues`, `schedsvc/scheduler.go:1426`, `schedsvc/self_improve.go:252`).
  - *Issue description:* the PRD link patch (`GetIssue`, `UpdateIssueDescription`, `forgesvc/prd_link_patch.go:159`, `:180`).
  - *Autopilot:* label events (`ListIssueLabelEvents`, `poller/autopilot.go:140`).
  - *Comments out:* see below.
- **The privilege check is code-host only.** `VerifyToken`, `ProjectRole`, `DefaultBranchProtection` (`api/internal/privcheck/checker.go:77`, `:139`, `:147`; `privcheck/service.go:256-283` at claim), fail-closed.
- **Mixed callers.** Comment intake reads issue comments (`ListIssueComments`) and checks each author against the repository (`RepositoryAuthorEligibility`, `api/internal/issueinput/assessment.go:86`); merge request watchers read the MR from the code host (`forgesvc/mr_watch.go:60`) and move the card on the issue side (`AutoMove`, `mr_watch.go:227`).
- **Issue identity is a number.** `issues.forge_issue_iid bigint` (`api/internal/store/migrations/00002_forge.sql:47-50`), `runs.issue_iid`, and one active run per issue on `(repo_id, coalesce(issue_iid, continued_issue_iid))` for issue runs and tasks continuing an issue (`00351_task_continued_issue.sql:11-14`).
- **Board columns are labels.** `board_columns.label_name` (`00002_forge.sql:39`); moving a card swaps labels through `UpdateIssueLabels` (`api/internal/forgesvc/service.go:512-542`), forge-first, with the `move_pending_since` marker (`api/internal/runlifecycle/lifecycle.go:1-17`).
- **Comments in.** Snapshotted once at run creation (`api/internal/issueinput/projection.go:149-169`, `00133_run_issue_comments.sql:4`). Steering exists only through `POST /api/runs/{id}/inputs` (`api/internal/handler/runs_lifecycle.go:29-33`).
- **Comments out.** uzi comments on the issue only in automatic cases, one new comment each from fixed templates: autopilot refusals (`poller/autopilot.go:310`) and the autopilot run's final outcome (`runlifecycle/lifecycle.go:486`, autopilot runs only), CI autofix start and halt (`poller/ci_autofix.go:256`, `:301`), merge request rework halt (`poller/mr_review_watch.go:418`). A run started by hand writes nothing on its issue. A failed post is not retried. `forge.IssueComment` carries `CreatedAt` only, no `UpdatedAt`, so an edited comment cannot be told apart today.
- **Withdrawal.** The full reconcile is the only path that observes de-labeling, and it evicts the card (`forgesvc/service.go:780-795`); no driver reports an issue moved to another project; no path cancels the issue's run.
- **Autopilot.** Maps the label adder to a uzi user through `forge_connections.human_username` (`00037_autopilot_mapping.sql:6-20`, `poller/autopilot.go:194`).

## Decisions

- **D1. The split.** `IssueTracker` = the eleven issue-side methods listed above, plus `MoveToColumn` (D6) and `UpdateIssueNote` (D12). `CodeHost` = the other 29. `RepositoryAuthorEligibility` and `UserExists` stay on `CodeHost`: they ask the forge about its accounts and permissions, not about an issue.
- **D2. `Forge` stays as the embedded union of `IssueTracker` and `CodeHost`.** The three drivers and `forgetest.BaseFake` keep implementing `Forge`, so they satisfy both halves.
- **D3. Narrowest interface per caller.** `privcheck`, the run claim and settle paths, pipeline sync and the merge request watchers take `CodeHost`. Every issue-side use listed in Current state takes `IssueTracker`: sync, the board, close and reopen, run creation, the worker's issue endpoints, the issue page, every place uzi creates an issue (board, findings, judge, review, self-improve, proposals), the PRD link patch, autopilot and the comment sites. A caller that needs both takes two parameters (`ch forge.CodeHost, tr forge.IssueTracker`), never the union.
- **D4. The split is pinned, at compile time first.** Each driver (`github.go`, `gitlab.go`, `forgejo.go`) and `forgetest.BaseFake` assert both halves next to their type (`var _ IssueTracker = (*github)(nil)`); passing an `IssueTracker` where a `CodeHost` is required is a compile error. The structural test arrives in M3, where it guards something the compiler cannot: the two halves share no method, `Forge` adds none, and no `IssueTracker` method takes a bare number first.
- **D5. Neutral references.** `IssueTracker` methods take `ProjectRef{ID int64; Key string}` and `IssueRef{Project ProjectRef; IID int64; Key string}` instead of bare numbers. Forge drivers build them from the numbers they already use, with the decimal number as the key.
- **D6. Moving a card is a `IssueTracker` method.** `MoveToColumn(ctx, IssueRef, ColumnMove)`. The forge drivers implement it with one shared helper that does today's label swap, in one `UpdateIssueLabels` call; `runlifecycle` and the board handlers call it instead of computing labels themselves.
- **D7. Text issue keys, additive.** `issues` and `runs` gain a nullable `issue_key text` (empty string refused). NULL means a forge issue whose key is its decimal number. One active run per issue moves to the effective key, `COALESCE(issue_key, issue_iid::text)`, keeping the `continued_issue_iid` fallback of today's index. No backfill; forge rows keep NULL.
- **D8. A repo resolves its two sides.** `IssueTrackerForRepo` and `CodeHostForRepo` return the issue side and the code side of a repo. In this PRD both return the repo's forge driver; phase 2 makes `IssueTrackerForRepo` return an external issue tracker when one is mapped.
- **D9. Comments reach the agent.** Each sync delivers new comments on the run's issue through `/inputs`: as `answer` while the run waits on a single question, otherwise as `follow_up`. Excluded: comments by the connection's bot, uzi's own comments and `/uzi` commands (D10). Delivered text is untrusted data, bounded like `issue_comments`, and the run view marks each delivered comment.
- **D10. `/uzi` commands from the issue.** A comment whose first line is `/uzi approve r<N>`, `/uzi reject r<N> [reason]`, `/uzi revise r<N> <request>`, `/uzi answer q<tag> <text>` or `/uzi cancel` acts like the same input in uzi, when its author is the run's owner, mapped through `human_username` as for autopilot. A plan or answer command must follow uzi's comment for that plan revision or question and target the current gate; `/uzi cancel` works at any point while the run is live. Every command must be unedited; otherwise uzi replies with a fixed refusal.
- **D11. Automatic withdrawal.** Removing the `uzi` label or closing the issue cancels its live run, with a final comment; the full reconcile that already evicts the card does it. An issue moved to another project is taken out the same way; if it lands in another enabled repo with the label, it becomes a new run there. A label put back creates a new run.
- **D12. One living comment, retried writes.** `UpdateIssueNote(ctx, IssueRef, noteID, body)` joins `IssueTracker`. uzi keeps one living status comment per run part and edits it on every change; new comments only for a plan to approve, a question and the final result. Every comment write goes through a per-run outbox: recorded before it is sent, with an invisible marker (run id and sequence number), read back before any retry, sent in order. After the last attempt the write becomes a visible sync error on the repo; the run is never blocked.
- **D13. Autopilot through the issue side.** The autopilot detector reads label events through `IssueTracker` and keeps mapping the label adder through `human_username`. Behaviour is unchanged.

## Modules and seams

### 1. The forge layer: `api/internal/forge`

- **Interfaces (M1):** one file per interface, each with the types only it uses: `IssueTracker` in `issuetracker.go`, `CodeHost` in `codehost.go`. `forge.go` keeps only the shared pieces (the `Forge` union, `Type`, `Project`, `BotIdentity`, `RateLimitError`, the sentinel errors, `New`, `timeoutClient`) (D1, D2). The three drivers (`gitlab.go`, `github.go`, `forgejo.go`) and `forgetest.BaseFake` keep implementing `Forge` and gain only their compile-time assertions (D4).
- **File layout rule for every milestone:** a new seam type or helper goes in the file named after its concern (for example the M3 references beside `IssueTracker` in `issuetracker.go`); a new driver method goes in that driver's existing per-concern file (`github_issues.go`, `forgejo_issues.go`, ...). State the file layout in each milestone's plan.
- **References (M3):** `ProjectRef`, `IssueRef` and `ColumnMove` live beside the interfaces; the drivers build them from the numbers they use today (D5).
- **Moving a card (M3):** `MoveToColumn` on `IssueTracker`, implemented in the three drivers through one shared helper that does today's label swap in one `UpdateIssueLabels` call (D6).
- **New driver methods (M4, M5):** `UpdateIssueNote` (D12) and `IssueComment.UpdatedAt` (D10), in the three drivers and `forgetest.BaseFake`.

### 2. Resolving a repo's two sides: `api/internal/forgesvc`

- `IssueTrackerForRepo` and `CodeHostForRepo` sit beside `ForgeForConnection` (`forgesvc/service.go:499`); in this PRD both return the driver it builds today (D8).
- Sync, the board, close and reopen and the PRD link patch take `IssueTracker`; the merge request watchers take `CodeHost` to read the MR and `IssueTracker` to move the card (D3). (M2 refined this: the PRD link patch also reads the MR and takes both halves; see the Decision Log, 2026-11-10.)

### 3. Callers on their half (M2)

- **`CodeHost` only:** `privcheck`, the run claim and settle paths, pipeline sync and the merge request watchers' MR reads (D3).
- **`IssueTracker` only:** every issue-side use listed in Current state, in `forgesvc`, `handler`, `issueinput`, `workersvc`, `schedsvc` and `poller` (D3, D13).
- **Both:** comment intake (`issueinput/assessment.go`) and the merge request watchers take two parameters, never the union.

### 4. Board columns: `api/internal/runlifecycle` and the board handlers (M3)

- `runlifecycle` and `handler/board.go` call `MoveToColumn` instead of computing label sets; `board_columns.label_name` and the `move_pending_since` marker are unchanged (D6).

### 5. Issue keys: `api/internal/store/migrations` (M3)

- One additive migration (number assigned at merge): nullable `issue_key` on `issues` and `runs`, empty string refused, and the one-active-run index on the effective key with the `continued_issue_iid` fallback (D7). No backfill.

### 6. Comments out: the per-run outbox (M4)

- Every comment write is recorded before it is sent, carries the invisible marker (run id and sequence number), is read back before any retry and is sent in order; after the last attempt it becomes a sync error on the repo (D12).
- Today's comment sites (`poller/autopilot.go`, `poller/ci_autofix.go`, `poller/mr_review_watch.go`, `runlifecycle/lifecycle.go`) write through the outbox; the living status comment is edited with `UpdateIssueNote`.

### 7. Withdrawal (M4)

- The full reconcile that evicts the card today (`forgesvc/service.go:780-795`) also cancels the issue's live run, with a final comment, on de-labeling, closing and moving to another project (D11).

### 8. Comments in and `/uzi` commands (M5)

- **Delivery:** each sync sends new comments on a live run's issue through the same path as `POST /api/runs/{id}/inputs` (`workersvc.SubmitInput`), as `answer` or `follow_up`, with the exclusions and bounds of D9.
- **Commands:** a parser for the first line of a comment maps `/uzi approve`, `reject`, `revise`, `answer` and `cancel` to the input kinds of the same name (`approve_plan`, `reject_plan`, `revise_plan`, `answer`, `cancel`); the author is checked against the run's owner through `human_username`; anything else gets the fixed refusal (D10).

## Testing decisions

- **Every milestone's gate:** `task gate:api` (`-race`, `-count=1`), plus `task gate:web` when web changes, `check:changelog-entry`, and `docs:sync` with `check-docs:web` when docs change. Each new behaviour gets a focused regression test watched red on today's code, then green. Token-shaped test strings are assembled at runtime. Nothing touches `.github/workflows/**`.
- **The split is pinned (M1):** a test over the method sets asserts that `IssueTracker` and `CodeHost` are disjoint and that together they equal `Forge`; moving one method to the wrong side turns it red. A second assertion checks that an `IssueTracker` value does not satisfy `CodeHost`.
- **Every issue-side use is on `IssueTracker` (M2):** the type-checked inventory used for Current state (`golang.org/x/tools/go/packages`) is rerun; all 47 uses must resolve through `IssueTracker`, and no production package outside `api/internal/forge` takes `forge.Forge` where one half suffices. `privcheck` compiles against `CodeHost` only.
- **No change in behaviour for M1 to M3:** the existing API, web, agent and E2E suites pass unchanged; a test that has to change in these milestones is a finding, not an update.
- **Board moves are identical (M3):** a recording fake built on `forgetest.BaseFake` asserts that `MoveToColumn` sends the same `UpdateIssueLabels` add and remove sets as today's `AutoMove` for every column transition, on the three forge types.
- **Text keys (M3, live DB):** the migration applies and rolls back; forge rows keep `issue_key` NULL; the effective-key unique index refuses a second active run for the same issue, through `issue_iid`, `issue_key` and the `continued_issue_iid` fallback.
- **Driver contract (M4, M5):** `UpdateIssueNote` and `IssueComment.UpdatedAt` are tested in each driver's existing mock-server tests (`gitlab_test.go`, `github_test.go`, `forgejo_test.go`), and in the shared fake (`task test:forge-fake`, `task test:forge-fake-contract`).
- **Writes land once, in order (M4):** the outbox is tested against forge-fake fault injection (`POST /_e2e/faults`): after the forge recovers every write lands, in order, with no duplicate, also across a requeue. Mutation: drop the read-before-retry check and watch the duplicate test go red.
- **Withdrawal (M4):** on the three forge fakes, removing the label, closing the issue and moving it to another project each cancel the live run with a final comment; a move into another enabled repo with the label starts a run there; a label put back creates a new run.
- **Comments and commands (M5):** a comment posted during a live run is delivered as `follow_up`, or as `answer` while the run waits on one question; bot comments, uzi's own comments and `/uzi` commands are never delivered. A command from the run's owner applies, `/uzi cancel` included; one from anyone else, an edited one or one for a stale gate gets the fixed refusal and changes nothing.
- **E2E:** each PR greps `e2e/` and `e2e/forge-fake/` for the comment templates and label expectations it changes, updates them in the same PR, runs `task test:e2e-lib` and `task test:forge-fake`, and names the phases left unrun.

## Milestones

Progress (one PR per milestone, each merged before the next starts):

- [x] **M1:** the `IssueTracker` and `CodeHost` interfaces (wise-systems/uzi#1)
- [x] **M2:** every caller on its half (PR for wise-systems/uzi#2). Verified in the run: the type-checked inventory shows 0 issue-side calls through `forge.Forge` and 45 through `IssueTracker` (plus `GetIssueBounded` and `EnsureBoardColumns`), and handing `privcheck` an `IssueTracker`-only value fails to compile. `task gate:api` slots green except `deadcode:api` (not run). The full E2E phases are pending exact-head PR CI.
- [ ] **M3:** neutral references, `MoveToColumn` and text keys
- [ ] **M4:** one living comment, retried writes and automatic withdrawal
- [ ] **M5:** comments and `/uzi` commands from the issue

### M1: the `IssueTracker` and `CodeHost` interfaces

D1, D2 and D4. The two interfaces in `api/internal/forge`, each in its own file, `Forge` as their union, the compile-time assertions; `AGENTS.md` "Forge layer" and `ARCHITECTURE.md` updated; changelog fragment.

- **Blocked by:** none.
- **Acceptance:** the three drivers and `forgetest.BaseFake` compile with only their assertions added; the moves are pure (`git diff -M` shows no changed lines in moved code); no caller changes; every existing test passes unchanged.

### M2: every caller on its half

D3, D8 and D13. `privcheck`, claim and settle, pipeline sync and the merge request watchers on `CodeHost`; every issue-side use from Current state on `IssueTracker`, autopilot included; mixed callers on two parameters; `IssueTrackerForRepo` and `CodeHostForRepo`.

- **Blocked by:** M1.
- **Acceptance:** the inventory shows all 47 issue-side uses on `IssueTracker`; `privcheck` cannot be handed a value that has only the issue side; existing autopilot tests pass through `IssueTracker`; every existing test and E2E phase passes unchanged.

### M3: neutral references, `MoveToColumn` and text keys

D4 (structural test), D5, D6 and D7. `ProjectRef` and `IssueRef` on every `IssueTracker` method; `MoveToColumn` with the shared label-swap helper, called by `runlifecycle` and the board handlers; the additive `issue_key` migration (number assigned at merge) and the effective-key index.

- **Blocked by:** M2.
- **Acceptance:** the structural test (D4) fails if the two halves share a method, if `Forge` adds one, or if an `IssueTracker` method takes a bare number first; the recorded-call test shows identical label calls; the migration applies and rolls back on a live DB, and every existing query returns the same rows.

### M4: one living comment, retried writes and automatic withdrawal

D11 and D12. `UpdateIssueNote` in the three drivers and the fake; the per-run outbox; today's comment sites moved onto it; cancellation on de-labeling, closing and moving; user docs for the living comment and withdrawal.

- **Blocked by:** M3.
- **Acceptance:** a full run leaves one living comment per run part, plus one comment per plan, question and final result; with forge-fake failing for a while, every write lands after recovery, in order, never twice; removing the label, closing the issue and moving it each cancel the live run with a final comment, on the three forge fakes.

### M5: comments and `/uzi` commands from the issue

D9 and D10. Comment delivery through `/inputs`; `IssueComment.UpdatedAt` in the three drivers; the command parser, owner check through `human_username` and fixed refusals; user docs for comments and commands on issues.

- **Blocked by:** M4.
- **Acceptance:** a comment on the issue reaches the live run within one poll interval; a valid command from the owner applies, `/uzi cancel` included; any other command gets the fixed refusal and changes nothing. Live acceptance (maintainer-owned): runs, board moves, the living comment, comments to the agent, `/uzi` commands, withdrawal and autopilot verified on GitHub, GitLab and Forgejo.

## Risks

- **A large mechanical diff (M1 to M3).** Every issue-side use changes its parameter type and then its references; the type-checked inventory of the 47 uses and the unchanged test suites are the guard.
- **E2E expectations (M4).** The living comment and the outbox change the comments uzi writes; every PR updates the templates and label expectations in `e2e/` and `e2e/forge-fake/` that it changes.
- **Forge rate limits (M4).** Editing the living comment on every change adds writes to the forge; the outbox sends them in order and turns the last failed attempt into a sync error instead of blocking the run.
- **Comment text reaches the agent (M5).** Comments are untrusted data; D9 bounds them like `issue_comments` and excludes bots, uzi's own comments and commands.
- **Withdrawal cancels work (M4).** Closing an issue by mistake cancels its live run; putting the label back creates a new run, not a resume (D11).

## Decision Log

- **2026-11-05: phase 1 also carries the new behaviours on GitLab, GitHub and Forgejo** (the living comment with retried writes, automatic withdrawal, comments that reach the agent and `/uzi` commands, M4 and M5), not only the split.
- **2026-11-05: the interface is named `IssueTracker`,** and the PRD says "issue tracker" throughout.
- **2026-11-05: phase 1 names no external issue tracker.** Phase 2 (connections, Settings → Issue trackers, board mapping) is a separate PRD.
- **2026-11-10: one file per interface.** `issuetracker.go` and `codehost.go`, with `forge.go` keeping only the shared pieces; r1 had both interfaces in `forge.go`, which review rejected as a split in name only.
- **2026-11-10: D4 moves from a reflection test to compile-time assertions in M1.** The assertions sit next to each driver and `forgetest.BaseFake`. The structural test (disjoint halves, nothing extra on `Forge`, no bare-number first parameter) is written in M3, where the bare-number rule gives it something the compiler cannot check.
- **2026-11-10: the PRD lives at `prds/ws-2-issue-tracker-codehost-split.md`.** Work moved from the internal GitLab tracker to wise-systems/uzi on GitHub; M1 landed as wise-systems/uzi#1.
- **2026-11-10: M2 test edits are mechanical only (owner decision).** Narrowing a method that a test fake implements changes that fake's parameter type (`forge.Forge` to the half), and a caller that needs both halves is called with the same fake twice (`f, f`; an inline literal fake is first hoisted into a local). No assertion or expectation changes; "every existing test passes unchanged" in M2 is read with these two mechanical classes allowed.
- **2026-11-10: M2 wires `IssueTrackerForRepo`/`CodeHostForRepo` (D8) where a repo is in hand without new test fakes.** They are `forgesvc.(*Service)` methods in `forgesvc/repo_sides.go` taking `(ctx, repoID, forgeType, baseURL, tokenCiphertext)` (the caller's own owner-scoped lookup, never the unscoped `GetRepoByID`), used by the repo-scoped handlers and the merged-predecessor check. `workersvc`, `schedsvc`, `runlifecycle`, `privcheck`, `provisionsvc` and `seed` keep their own `ForgeForConnection` factory interfaces (adding the resolvers there would add methods to about 30 test fakes) and narrow at the first use; phase 2 routes them through the resolvers. Connection-scoped handlers, the run-scoped worker forge proxy (`resolveForgeRun`), `ProjectSyncService`'s own factory and the poll loop build one driver and narrow it at each use or pass it as both halves.
- **2026-11-10: the PRD link patch takes both halves.** D3 placed it on `IssueTracker`, but it also reads the merge request (`GetMergeRequest`), so `SyncPRDLinkPatches` takes `(ch forge.CodeHost, tr forge.IssueTracker)`. Comment intake (`issueinput.Fetch`), the autopilot, CI-autofix and MR-rework detectors, the MR state watcher, self-improve and the worker get-issue route are mixed the same way.
- **2026-11-10: "`privcheck` compiles against `CodeHost` only" means every privcheck parameter, local and forge call is `CodeHost`.** Its `ForgeBuilder` factory interface still returns `forge.Forge`, converted to `CodeHost` straight after the build; handing privcheck an `IssueTracker`-only value is a compile error.
