-- +goose Up
-- Issue #18: Postgres 17.12 and later refuse any table-rewriting ALTER on runs (a defaulted or
-- generated ADD COLUMN, a column type change) while a function with an SQL-standard body reads
-- the runs row type. fn_run_admitted_during_intake_pause (00358) was the only such function.
-- 00362, 00363, 00370, 00394, 00397 and 00407 convert it before their own ALTERs; this converts
-- it on a database that applied all of those before the fix, so the next migration that adds a
-- defaulted column to runs still applies after a Postgres minor upgrade. Same signature,
-- volatility and expression; idempotent.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fn_run_admitted_during_intake_pause(r runs) RETURNS boolean
LANGUAGE sql STABLE PARALLEL SAFE AS $$
 SELECT r.claim_generation > 0 OR r.kind = 'cross_check'
$$;
-- +goose StatementEnd

-- +goose Down
-- Deliberately keeps the string body: restoring the SQL-standard body would bring back the
-- 17.12 refusal for every later runs ALTER. The two bodies answer identically.
SELECT 1;
