-- +goose Up

-- CREATE TRIGGER takes SHARE ROW EXCLUSIVE on repos. lock_timeout bounds only the wait for a
-- lock, not how long it is held: fail fast instead of queueing behind a long transaction and
-- stalling every reader (the 00402 pattern).
SET LOCAL lock_timeout = '5s';

-- PRD #4173 D13: a repo's issue source must not change under its history. Removing a repo
-- deletes its row, and the next membership listing inserts it again as awaiting its first
-- enable, which would make a repo used for months linkable to an issue tracker (and a linked
-- repo a plain forge repo). One marker per removed project remembers that it was used and
-- whether it was linked.
--
-- No foreign key on connection_id: a foreign key would make the cascade from a forge
-- connection delete fail while it deletes the repos. The rows of a deleted connection are
-- inert, because connection ids are never reused.
CREATE TABLE repo_removal_markers (
    connection_id    uuid        NOT NULL,
    forge_project_id bigint      NOT NULL,
    was_linked       boolean     NOT NULL,
    removed_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, forge_project_id)
);

-- Records the marker when a previously enabled repo row is deleted. A repo that never left
-- its first-enable wait wrote nothing an issue source could depend on, so it writes no marker.
-- A second removal of the same project keeps a linked flag already set. A BEFORE DELETE
-- trigger must return OLD: a NULL return skips the delete.
-- +goose StatementBegin
CREATE FUNCTION repos_removal_marker_record() RETURNS trigger AS $$
BEGIN
    IF NOT OLD.awaiting_first_enable THEN
        INSERT INTO repo_removal_markers (connection_id, forge_project_id, was_linked)
        VALUES (OLD.connection_id, OLD.forge_project_id, OLD.issue_tracker_connection_id IS NOT NULL)
        ON CONFLICT (connection_id, forge_project_id) DO UPDATE
        SET was_linked = repo_removal_markers.was_linked OR EXCLUDED.was_linked,
            removed_at = now();
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER repos_removal_marker_record
    BEFORE DELETE ON repos
    FOR EACH ROW EXECUTE FUNCTION repos_removal_marker_record();

-- A project that has a marker is listed again as already used, never awaiting its first
-- enable. Triggers of one event fire in name order, so repos_awaiting_first_enable_guard
-- (00425) runs first and this one second; the guard only clears the marker column or refuses
-- setting it back, and clearing it here is always allowed, so the order does not matter.
-- +goose StatementBegin
CREATE FUNCTION repos_removal_marker_relist() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM repo_removal_markers m
               WHERE m.connection_id = NEW.connection_id AND m.forge_project_id = NEW.forge_project_id) THEN
        NEW.awaiting_first_enable := false;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER repos_removal_marker_relist
    BEFORE INSERT ON repos
    FOR EACH ROW EXECUTE FUNCTION repos_removal_marker_relist();

-- A project that was linked before its removal may not be enabled unlinked: its issues were
-- numbered by an issue tracker, and the forge's numbers would collide with the agent branches
-- and merge requests they named. Reads only OLD, NEW and the marker, never
-- awaiting_first_enable, which the 00425 guard rewrites. SQLSTATE UZ414 is this refusal's own
-- code (class UZ is not a PostgreSQL class); the provisioning service maps it.
-- +goose StatementBegin
CREATE FUNCTION repos_removal_marker_enable_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.enabled AND NOT OLD.enabled AND NEW.issue_tracker_connection_id IS NULL
       AND EXISTS (SELECT 1 FROM repo_removal_markers m
                   WHERE m.connection_id = NEW.connection_id
                     AND m.forge_project_id = NEW.forge_project_id
                     AND m.was_linked) THEN
        RAISE EXCEPTION 'repo % was linked to an issue tracker before it was removed: it cannot be enabled unlinked', NEW.id
            USING ERRCODE = 'UZ414';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER repos_removal_marker_enable_guard
    BEFORE UPDATE OF enabled ON repos
    FOR EACH ROW EXECUTE FUNCTION repos_removal_marker_enable_guard();

-- +goose Down
DROP TRIGGER repos_removal_marker_enable_guard ON repos;
DROP FUNCTION repos_removal_marker_enable_guard();
DROP TRIGGER repos_removal_marker_relist ON repos;
DROP FUNCTION repos_removal_marker_relist();
DROP TRIGGER repos_removal_marker_record ON repos;
DROP FUNCTION repos_removal_marker_record();
DROP TABLE repo_removal_markers;
