package store

import (
	"regexp"
	"strings"
	"testing"
)

// Issue #18: Postgres 17.12 and later refuse a table-rewriting ALTER on a table whose row type
// is read by a function with an SQL-standard (BEGIN ATOMIC) body. These tests pin the shape of
// the migration fix without a database: only 00358 may create such a function, and every
// migration that adds a defaulted column to runs converts the function first.

const intakePauseFn = "fn_run_admitted_during_intake_pause"

var (
	beginAtomicRE = regexp.MustCompile(`(?i)\bbegin\s+atomic\b`)
	// The other SQL-standard form: LANGUAGE sql followed by a bare RETURN <expr> body.
	sqlReturnBodyRE   = regexp.MustCompile(`(?is)\blanguage\s+sql\b[^;$]*?\breturn\b`)
	convertFnRE       = regexp.MustCompile(`(?i)create\s+or\s+replace\s+function\s+` + intakePauseFn + `\b`)
	runsAlterRE       = regexp.MustCompile(`(?is)^\s*alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?runs\b`)
	defaultedAddColRE = regexp.MustCompile(`(?is)\badd\s+column\s+(?:if\s+not\s+exists\s+)?[^,;]*?\bdefault\b`)
)

// stripSQLComments drops full-line "--" comments, keeping every other byte (and line breaks).
func stripSQLComments(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

// upSection returns the comment-stripped text before the "-- +goose Down" line.
func upSection(s string) string {
	if i := strings.Index(s, "-- +goose Down"); i >= 0 {
		s = s[:i]
	}
	return stripSQLComments(s)
}

func readMigrations(t *testing.T) map[string]string {
	t.Helper()
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

func TestNoSQLStandardBodyMigrationExceptIntakePause(t *testing.T) {
	for name, body := range readMigrations(t) {
		if strings.HasPrefix(name, "00358_") {
			continue
		}
		code := stripSQLComments(body)
		if beginAtomicRE.MatchString(code) || sqlReturnBodyRE.MatchString(code) {
			t.Errorf("%s contains an SQL-standard function body (BEGIN ATOMIC or LANGUAGE sql RETURN): Postgres 17.12+ refuses table-rewriting ALTERs "+
				"(defaulted ADD COLUMN, column TYPE change) on a table whose row type such a function reads, "+
				"so a later migration could no longer alter that table (issue #18); use a string ($$) body", name)
		}
	}
}

func TestIntakePauseFunctionConvertedBeforeDefaultedRunsColumn(t *testing.T) {
	migs := readMigrations(t)
	find := func(match func(string) bool) (string, string) {
		for name, body := range migs {
			if match(name) {
				return name, body
			}
		}
		return "", ""
	}

	for _, prefix := range []string{"00362_", "00363_", "00370_", "00394_", "00397_", "00407_"} {
		name, body := find(func(n string) bool { return strings.HasPrefix(n, prefix) })
		if name == "" {
			t.Fatalf("no embedded migration with prefix %s", prefix)
		}
		up := upSection(body)
		conv := convertFnRE.FindStringIndex(up)
		if conv == nil {
			t.Errorf("%s: Up does not CREATE OR REPLACE %s; Postgres 17.12+ would refuse its defaulted ALTER TABLE runs (issue #18)", name, intakePauseFn)
			continue
		}
		alter := -1
		off := 0
		for _, stmt := range strings.SplitAfter(up, ";") {
			if runsAlterRE.MatchString(stmt) {
				if m := defaultedAddColRE.FindStringIndex(stmt); m != nil {
					alter = off + m[0]
					break
				}
			}
			off += len(stmt)
		}
		if alter < 0 {
			t.Errorf("%s: no ALTER TABLE runs ... ADD COLUMN ... DEFAULT found in Up; the ordering check is stale", name)
			continue
		}
		if conv[0] > alter {
			t.Errorf("%s: %s is converted after the first defaulted ADD COLUMN on runs; it must come before, or Postgres 17.12+ refuses the ALTER (issue #18)", name, intakePauseFn)
		}
	}

	name, body := find(func(n string) bool { return strings.HasSuffix(n, "_intake_pause_admission_string_body.sql") })
	if name == "" {
		t.Fatal("no embedded migration named *_intake_pause_admission_string_body.sql")
	}
	if !convertFnRE.MatchString(upSection(body)) {
		t.Errorf("%s: Up does not CREATE OR REPLACE %s", name, intakePauseFn)
	}
}
