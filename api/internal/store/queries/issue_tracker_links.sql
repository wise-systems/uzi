-- Repo to issue tracker project links (PRD #4173 M4, DL-3..DL-5).

-- name: GetRepoIssueSourceForUser :one
-- The one owner-scoped row the per-repo resolvers read (DL-5): the repo, its forge
-- connection and its issue tracker link. The binding is keyed by repo row id and the
-- owning user, never by forge project number. linked_connection_id is the link's
-- connection ONLY when it belongs to the same user as the forge connection; a linked
-- repo whose connection is missing for that owner reads NULL here and the resolver
-- fails closed.
SELECT r.id, r.forge_project_id, r.awaiting_first_enable,
       r.issue_tracker_connection_id, r.issue_tracker_project_id, r.issue_tracker_project_short,
       r.eligibility_tag_id, r.eligibility_tag_name, r.issue_tracker_linked_at,
       c.forge_type, c.base_url, c.token_ciphertext,
       itc.id AS linked_connection_id
FROM repos r
JOIN forge_connections c ON c.id = r.connection_id
LEFT JOIN issue_tracker_connections itc
       ON itc.id = r.issue_tracker_connection_id AND itc.user_id = c.user_id
WHERE r.id = $1 AND c.user_id = $2;

-- name: LockRepoForLinkForUser :one
-- Takes the repo row lock that the link and the first enable share (DL-4), scoped to
-- the owning user. Call inside a transaction; the returned markers are read after the
-- lock, so an enable that committed first is visible.
SELECT r.id, r.awaiting_first_enable, r.issue_tracker_connection_id
FROM repos r
JOIN forge_connections c ON c.id = r.connection_id
WHERE r.id = $1 AND c.user_id = $2
FOR UPDATE OF r;

-- name: RepoStoresIssueNumber :one
-- True when any table that stores an issue number for the repo has such a row. The
-- guard triggers (migration 00425) make this false for an awaiting repo; the link
-- checks it anyway as defence in depth. Keep in step with the trigger list there.
SELECT (
    EXISTS (SELECT 1 FROM issues WHERE issues.repo_id = $1 AND issues.forge_issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM runs WHERE runs.repo_id = $1 AND (runs.issue_iid IS NOT NULL OR runs.continued_issue_iid IS NOT NULL))
 OR EXISTS (SELECT 1 FROM run_schedules WHERE run_schedules.repo_id = $1 AND run_schedules.issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM autopilot_triggers WHERE autopilot_triggers.repo_id = $1 AND autopilot_triggers.issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM github_project_items WHERE github_project_items.repo_id = $1 AND github_project_items.forge_issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM issue_proposals WHERE issue_proposals.repo_id = $1 AND issue_proposals.created_issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM recommendation_filed_issues WHERE recommendation_filed_issues.filed_repo_id = $1 AND recommendation_filed_issues.filed_issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM finding_dispositions WHERE finding_dispositions.repo_id = $1 AND finding_dispositions.filed_issue_iid IS NOT NULL)
 OR EXISTS (SELECT 1 FROM finding_group_operations WHERE finding_group_operations.repo_id = $1 AND finding_group_operations.issue_iid IS NOT NULL)
)::boolean AS stores_issue_number;

-- name: SetRepoIssueTrackerLink :one
-- Writes the link. Only a repo that has never been enabled and is not yet linked
-- matches; no row means refused. Call after LockRepoForLinkForUser in the same
-- transaction.
UPDATE repos
SET issue_tracker_connection_id = @connection_id::uuid,
    issue_tracker_project_id    = @project_id::text,
    issue_tracker_project_short = @project_short::text,
    eligibility_tag_id          = @tag_id::text,
    eligibility_tag_name        = @tag_name::text,
    issue_tracker_linked_at     = now()
WHERE id = @repo_id AND awaiting_first_enable AND issue_tracker_connection_id IS NULL
RETURNING *;

-- name: CountReposLinkedToIssueTrackerConnection :one
-- How many repos name this issue tracker connection; deleting it is refused while any do.
SELECT count(*) FROM repos WHERE issue_tracker_connection_id = @connection_id::uuid;
