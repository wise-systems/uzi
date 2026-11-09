package forge

import (
	"reflect"
	"testing"
)

// Every driver implements both halves of the Forge contract (PRD #8). BaseFake
// carries the same assertions in forgetest/basefake.go, which this package
// cannot import.
var (
	_ IssueTracker = (*github)(nil)
	_ CodeHost     = (*github)(nil)
	_ IssueTracker = (*gitLab)(nil)
	_ CodeHost     = (*gitLab)(nil)
	_ IssueTracker = (*forgejo)(nil)
	_ CodeHost     = (*forgejo)(nil)
)

// TestIssueTrackerAndCodeHostDisjoint pins the one property the compiler does not:
// an interface may embed two interfaces that share an identical method, so a
// method declared on both halves would still build.
func TestIssueTrackerAndCodeHostDisjoint(t *testing.T) {
	tracker := reflect.TypeFor[IssueTracker]()
	host := reflect.TypeFor[CodeHost]()
	for i := range tracker.NumMethod() {
		name := tracker.Method(i).Name
		if _, ok := host.MethodByName(name); ok {
			t.Fatalf("method %s is on both IssueTracker and CodeHost", name)
		}
	}
}
