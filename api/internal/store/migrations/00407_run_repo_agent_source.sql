-- +goose Up
-- Issue #18: Postgres 17.12 and later refuse a defaulted ADD COLUMN on runs while the 00358
-- intake-pause admission function keeps its SQL-standard body over the runs row type.
-- Re-create it first with the equivalent string body: same signature, volatility and
-- expression. Idempotent: a fresh chain converts it in 00362 (the later copies are no-ops),
-- and a database that applied 00358 earlier converts it in whichever of 00362, 00363, 00370,
-- 00394, 00397, 00407 or 00426 it runs first.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fn_run_admitted_during_intake_pause(r runs) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
 SELECT r.claim_generation > 0 OR r.kind = 'cross_check'
$$;
-- +goose StatementEnd
ALTER TABLE runs ADD COLUMN repo_agent_source_commit text;
ALTER TABLE runs ADD CONSTRAINT runs_repo_agent_source_commit_check
    CHECK (repo_agent_source_commit IS NULL OR repo_agent_source_commit ~ '^[0-9a-f]{40}$') NOT VALID;
ALTER TABLE runs ADD COLUMN repo_agent_source_required boolean NOT NULL DEFAULT false;
UPDATE runs SET repo_agent_source_required = true
WHERE status NOT IN ('completed', 'failed', 'cancelled')
  AND kind IN ('issue', 'ci_fix', 'mr_rework', 'prompt', 'self_improve', 'task')
  AND review_target_run_id IS NULL
  AND egress_profile_id IS NULL;

-- +goose Down
ALTER TABLE runs DROP COLUMN IF EXISTS repo_agent_source_required;
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_repo_agent_source_commit_check;
ALTER TABLE runs DROP COLUMN IF EXISTS repo_agent_source_commit;
