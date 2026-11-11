-- +goose Up

-- PRD #4173 M4: link a repo to an external issue tracker project, and keep that
-- link from ever being made once the repo has issue numbers of its own (D13).

-- The never-enabled marker (DL-3). A repo is "awaiting its first enable" from the
-- moment the membership listing discovers it (UpsertRepo sets it) until it is first
-- enabled. The column default is false, so every row that exists when this migration
-- runs counts as previously enabled and can never be linked.
ALTER TABLE repos ADD COLUMN awaiting_first_enable boolean NOT NULL DEFAULT false;

-- Clears the marker in the very statement that first enables the repo, on every
-- enable path (a raw UPDATE included), and refuses to set it back. No column list on
-- the trigger: it must see every UPDATE, because the refusal guards a write that
-- touches only awaiting_first_enable.
-- +goose StatementBegin
CREATE FUNCTION repos_awaiting_first_enable_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.enabled THEN
        NEW.awaiting_first_enable := false;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.awaiting_first_enable = false AND NEW.awaiting_first_enable = true THEN
        RAISE EXCEPTION 'repos.awaiting_first_enable is never set back once cleared (repo %)', NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER repos_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE ON repos
    FOR EACH ROW EXECUTE FUNCTION repos_awaiting_first_enable_guard();

-- The link from a repo to one project of one issue tracker connection, plus the
-- eligibility tag that selects which issues the sync reads. All six columns are set
-- together or not at all. The connection FK keeps the default NO ACTION (checked at
-- the end of the statement) so deleting a connection that a repo still names is
-- refused, while a user-delete cascade is not refused midway.
ALTER TABLE repos
    ADD COLUMN issue_tracker_connection_id uuid REFERENCES issue_tracker_connections,
    ADD COLUMN issue_tracker_project_id    text,
    ADD COLUMN issue_tracker_project_short text,
    ADD COLUMN eligibility_tag_id          text,
    ADD COLUMN eligibility_tag_name        text,
    ADD COLUMN issue_tracker_linked_at     timestamptz;

ALTER TABLE repos ADD CONSTRAINT repos_issue_tracker_link_shape CHECK (
    (issue_tracker_connection_id IS NULL
        AND issue_tracker_project_id IS NULL
        AND issue_tracker_project_short IS NULL
        AND eligibility_tag_id IS NULL
        AND eligibility_tag_name IS NULL
        AND issue_tracker_linked_at IS NULL)
    OR
    (issue_tracker_connection_id IS NOT NULL
        AND issue_tracker_project_id IS NOT NULL
        AND issue_tracker_project_short IS NOT NULL
        AND eligibility_tag_id IS NOT NULL
        AND eligibility_tag_name IS NOT NULL
        AND issue_tracker_linked_at IS NOT NULL
        AND char_length(issue_tracker_project_id) BETWEEN 1 AND 128
        AND char_length(issue_tracker_project_short) BETWEEN 1 AND 128
        AND char_length(eligibility_tag_id) BETWEEN 1 AND 128
        AND char_length(eligibility_tag_name) BETWEEN 1 AND 256
        AND issue_tracker_project_id !~ '[[:cntrl:]]'
        AND issue_tracker_project_short !~ '[[:cntrl:]]'
        AND eligibility_tag_id !~ '[[:cntrl:]]'
        AND eligibility_tag_name !~ '[[:cntrl:]]'
        AND issue_tracker_project_short ~ '^[A-Za-z][A-Za-z0-9_]*$')
);

-- The readable id of an issue that came from an issue tracker (DL-1), for example
-- ABC-12. NULL for forge issues. The suffix must equal the row's own number, so a
-- key cannot name a different issue than the row it sits on.
ALTER TABLE issues ADD COLUMN issue_key text;
ALTER TABLE issues ADD CONSTRAINT issues_issue_key_shape CHECK (
    issue_key IS NULL OR (
        char_length(issue_key) <= 64
        AND issue_key ~ '^[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]*$'
        AND issue_key LIKE '%-' || forge_issue_iid::text)
);

-- The issue-number guard (DL-4). One function for every table that stores an issue
-- number of a repo: a row with a non-NULL issue number whose repo is still awaiting
-- its first enable is refused, so no issue number can exist on a repo that could
-- still be linked. The link takes its own row lock; the marker only goes from true to
-- false, so a write that passed this guard proves the repo can never be linked later.
--
-- Trigger arguments: the repo id column name, then one or more issue number column
-- names. A NULL repo id (recommendation_filed_issues.filed_repo_id) is skipped.
-- SQLSTATE UZ413 is this guard's own code (class UZ is not a PostgreSQL class); the
-- store maps it, nothing else raises it.
-- +goose StatementBegin
CREATE FUNCTION issue_number_awaiting_first_enable_guard() RETURNS trigger AS $$
DECLARE
    j        jsonb := to_jsonb(NEW);
    repo_val text  := j ->> TG_ARGV[0];
    has_num  boolean := false;
    i        int;
BEGIN
    IF repo_val IS NULL THEN
        RETURN NEW;
    END IF;
    FOR i IN 1 .. TG_NARGS - 1 LOOP
        IF j ->> TG_ARGV[i] IS NOT NULL THEN
            has_num := true;
        END IF;
    END LOOP;
    IF NOT has_num THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM repos WHERE id = repo_val::uuid AND awaiting_first_enable) THEN
        RAISE EXCEPTION 'repo % has never been enabled: % may not store an issue number', repo_val, TG_TABLE_NAME
            USING ERRCODE = 'UZ413';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issues_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, forge_issue_iid ON issues
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'forge_issue_iid');
CREATE TRIGGER runs_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, issue_iid, continued_issue_iid ON runs
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'issue_iid', 'continued_issue_iid');
CREATE TRIGGER run_schedules_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, issue_iid ON run_schedules
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'issue_iid');
CREATE TRIGGER autopilot_triggers_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, issue_iid ON autopilot_triggers
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'issue_iid');
CREATE TRIGGER github_project_items_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, forge_issue_iid ON github_project_items
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'forge_issue_iid');
CREATE TRIGGER issue_proposals_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, created_issue_iid ON issue_proposals
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'created_issue_iid');
CREATE TRIGGER recommendation_filed_issues_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF filed_repo_id, filed_issue_iid ON recommendation_filed_issues
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('filed_repo_id', 'filed_issue_iid');
CREATE TRIGGER finding_dispositions_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, filed_issue_iid ON finding_dispositions
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'filed_issue_iid');
CREATE TRIGGER finding_group_operations_awaiting_first_enable_guard
    BEFORE INSERT OR UPDATE OF repo_id, issue_iid ON finding_group_operations
    FOR EACH ROW EXECUTE FUNCTION issue_number_awaiting_first_enable_guard('repo_id', 'issue_iid');

-- +goose Down
DROP TRIGGER finding_group_operations_awaiting_first_enable_guard ON finding_group_operations;
DROP TRIGGER finding_dispositions_awaiting_first_enable_guard ON finding_dispositions;
DROP TRIGGER recommendation_filed_issues_awaiting_first_enable_guard ON recommendation_filed_issues;
DROP TRIGGER issue_proposals_awaiting_first_enable_guard ON issue_proposals;
DROP TRIGGER github_project_items_awaiting_first_enable_guard ON github_project_items;
DROP TRIGGER autopilot_triggers_awaiting_first_enable_guard ON autopilot_triggers;
DROP TRIGGER run_schedules_awaiting_first_enable_guard ON run_schedules;
DROP TRIGGER runs_awaiting_first_enable_guard ON runs;
DROP TRIGGER issues_awaiting_first_enable_guard ON issues;
DROP FUNCTION issue_number_awaiting_first_enable_guard();
ALTER TABLE issues DROP CONSTRAINT issues_issue_key_shape;
ALTER TABLE issues DROP COLUMN issue_key;
ALTER TABLE repos DROP CONSTRAINT repos_issue_tracker_link_shape;
ALTER TABLE repos
    DROP COLUMN issue_tracker_linked_at,
    DROP COLUMN eligibility_tag_name,
    DROP COLUMN eligibility_tag_id,
    DROP COLUMN issue_tracker_project_short,
    DROP COLUMN issue_tracker_project_id,
    DROP COLUMN issue_tracker_connection_id;
DROP TRIGGER repos_awaiting_first_enable_guard ON repos;
DROP FUNCTION repos_awaiting_first_enable_guard();
ALTER TABLE repos DROP COLUMN awaiting_first_enable;
