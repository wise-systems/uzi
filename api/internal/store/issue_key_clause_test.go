package store_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestIssueKeyClaimClauseCountPinned pins where the standalone issue_key_v1 claim clause
// (PRD #4173) lives in queries/runtime.sql: at every site that carries the ci_config_guard_v1
// claim clause, and nowhere else. A new mirror of the claim gate that forgets it changes this
// table. The live-DB tests in workersvc prove the behaviour at each site.
func TestIssueKeyClaimClauseCountPinned(t *testing.T) {
	raw, err := os.ReadFile("queries/runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	clause := regexp.MustCompile(`'issue_key_v1' = ANY\(\S+\)\)`)
	guard := regexp.MustCompile(`NOT (?:r|run)\.ci_config_guard OR 'ci_config_guard_v1' = ANY\(`)

	got := map[string]int{}
	guards := map[string]int{}
	for _, block := range strings.Split(string(raw), "\n-- name: ")[1:] {
		name := strings.Fields(block)[0]
		if n := len(clause.FindAllString(block, -1)); n > 0 {
			got[name] = n
		}
		if n := len(guard.FindAllString(block, -1)); n > 0 {
			guards[name] = n
		}
	}
	want := map[string]int{
		"ClaimRun":                              2, // the claimant and the fleet-spread peer arm
		"CountOnlineWorkersClaimableForRun":     1,
		"ListUnplaceableQueuedRunsForEphemeral": 1,
		"ListSaturationQueuedRunsForEphemeral":  2,
	}
	for name, n := range want {
		if got[name] != n {
			t.Errorf("%s carries %d issue_key clauses, want %d", name, got[name], n)
		}
	}
	for name, n := range got {
		if name == "IssueKeyWorkerMissingForRepo" {
			if n != 1 {
				t.Errorf("health query mentions issue_key_v1 %d times, want 1", n)
			}
			continue
		}
		if want[name] == 0 {
			t.Errorf("%s carries %d unexpected issue_key clauses", name, n)
		}
	}
	for name, n := range guards {
		if got[name] != n {
			t.Errorf("%s mirrors the ci_config_guard clause %d times but the issue_key clause %d times", name, n, got[name])
		}
	}
}
