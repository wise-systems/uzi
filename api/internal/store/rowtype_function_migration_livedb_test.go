package store

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntakePauseAdmissionStringBodyMigrationLiveDB walks one throwaway database through every
// migration that converts fn_run_admitted_during_intake_pause (issue #18). Before each target
// it restores the SQL-standard body from 00358, so each migration is proven to convert the
// function by itself (prosqlbody IS NULL afterwards). On any 17.x that is the whole claim; on
// 17.12+ each MigrateTo additionally fails unless the migration converts the function before
// its defaulted ADD COLUMN on runs, and the final probe fails unless the head schema accepts
// one. The static ordering test covers older minors.
func TestIntakePauseAdmissionStringBodyMigrationLiveDB(t *testing.T) {
	_, dsn := cjCreateDatabase(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	migs := readMigrations(t)
	versionOf := func(match func(string) bool) int64 {
		t.Helper()
		for name := range migs {
			if match(name) {
				v, err := strconv.ParseInt(name[:strings.Index(name, "_")], 10, 64)
				if err != nil {
					t.Fatalf("parse version of %s: %v", name, err)
				}
				return v
			}
		}
		t.Fatal("expected migration not embedded")
		return 0
	}
	var targets []int64
	for _, p := range []string{"00362_", "00363_", "00370_", "00394_", "00397_", "00407_"} {
		targets = append(targets, versionOf(func(n string) bool { return strings.HasPrefix(n, p) }))
	}
	targets = append(targets, versionOf(func(n string) bool { return strings.HasSuffix(n, "_intake_pause_admission_string_body.sql") }))

	// The CREATE FUNCTION statement of 00358 (through its closing END;), verbatim, as OR REPLACE.
	var restore string
	for name, body := range migs {
		if !strings.HasPrefix(name, "00358_") {
			continue
		}
		re := regexp.MustCompile(`(?s)CREATE FUNCTION ` + intakePauseFn + `.*?\nEND;`)
		m := re.FindString(body)
		if m == "" {
			t.Fatal("00358: CREATE FUNCTION statement not found")
		}
		restore = strings.Replace(m, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
		if restore == m {
			t.Fatal("00358: CREATE FUNCTION replacement did not apply")
		}
	}
	if restore == "" {
		t.Fatal("00358 migration not embedded")
	}

	if err := MigrateTo(ctx, dsn, 361); err != nil {
		t.Fatalf("MigrateTo(361): %v", err)
	}
	pool := cjOpenPool(t, dsn)
	const hasSQLBody = `SELECT prosqlbody IS NOT NULL FROM pg_proc WHERE oid = 'fn_run_admitted_during_intake_pause(runs)'::regprocedure`
	for _, target := range targets {
		if _, err := pool.Exec(ctx, restore); err != nil {
			t.Fatalf("before %d: restore SQL-standard body: %v", target, err)
		}
		var atomic bool
		if err := pool.QueryRow(ctx, hasSQLBody).Scan(&atomic); err != nil || !atomic {
			t.Fatalf("before %d: restore did not yield an SQL-standard body (atomic=%v, err=%v)", target, atomic, err)
		}
		if err := MigrateTo(ctx, dsn, target); err != nil {
			t.Fatalf("MigrateTo(%d): %v", target, err)
		}
		var version int64
		if err := pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil || version != target {
			t.Fatalf("after MigrateTo(%d): goose version %d, err %v", target, version, err)
		}
		if err := pool.QueryRow(ctx, hasSQLBody).Scan(&atomic); err != nil || atomic {
			t.Fatalf("migration %d left the SQL-standard body in place (atomic=%v, err=%v)", target, atomic, err)
		}
	}

	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("Migrate to head: %v", err)
	}
	var sqlStandard []string
	rows, err := pool.Query(ctx, `SELECT p.oid::regprocedure::text FROM pg_proc p
		WHERE p.pronamespace = 'public'::regnamespace AND p.prosqlbody IS NOT NULL ORDER BY 1`)
	if err != nil {
		t.Fatalf("list SQL-standard function bodies: %v", err)
	}
	for rows.Next() {
		var fn string
		if err := rows.Scan(&fn); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sqlStandard = append(sqlStandard, fn)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(sqlStandard) > 0 {
		t.Fatalf("head schema has SQL-standard function bodies %v; Postgres 17.12+ refuses table-rewriting ALTERs on a row type they read (issue #18)", sqlStandard)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "ALTER TABLE runs ADD COLUMN issue18_probe integer NOT NULL DEFAULT 0"); err != nil {
		t.Fatalf("defaulted ADD COLUMN on runs at head: %v", err)
	}
}
