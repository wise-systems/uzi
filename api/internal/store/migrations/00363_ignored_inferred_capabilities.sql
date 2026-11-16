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
ALTER TABLE repos ADD COLUMN ignored_inferred_capabilities text[] NOT NULL DEFAULT '{}';
-- New runs capture the repo policy in the same INSERT snapshot as the declared hint.
-- Empty defaults preserve existing runs and repo-less run kinds.
ALTER TABLE runs ADD COLUMN ignored_inferred_capabilities text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE runs DROP COLUMN ignored_inferred_capabilities;
ALTER TABLE repos DROP COLUMN ignored_inferred_capabilities;
