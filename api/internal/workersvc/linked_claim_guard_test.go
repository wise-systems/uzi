package workersvc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
	"github.com/vtmocanu/uzi/api/internal/privcheck"
	"github.com/vtmocanu/uzi/api/internal/store"
)

// PRD #4173 M4 acceptance, claim half: for an issue run on a repo linked to an issue
// tracker, the claim backstop (assembleClaim's privcheck.GuardRepo call, driven through
// Service.Claim with the real privcheck service over a guardForge) still runs against
// the forge only. It clears a healthy forge without one issue tracker request, and
// fails closed (no payload, the run failed with the guardrail origin) when the forge
// cannot be read, with the issue tracker unreachable in both cases. The link itself is
// not part of the claim context row: the issue tracker is wired on the service so any
// call into it would be recorded or fail.
func TestLinkedRepoClaimGuardRunsAgainstTheForgeOnlyAndFailsClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		verifyErr error
		wantBlock bool
	}{
		"healthy forge clears the claim":  {},
		"failing forge refuses the claim": {verifyErr: errors.New("401 token revoked"), wantBlock: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := youtracktest.NewServer(t)
			yt := youTrackDriver(t, srv)
			srv.Close() // the issue tracker is unreachable
			srv.ResetRequests()

			f := newClaimFixture(t)
			g := &guardForge{verifyErr: tc.verifyErr}
			f.svc.SetRepoGuard(privcheck.NewService(nil, m2Builder{g}))
			f.svc.SetIssueSources(&fakeSources{tr: yt})

			payload, err := f.svc.Claim(context.Background(), store.Worker{ID: uuid.New(), UserID: f.owner}, nil)
			if err != nil {
				t.Fatalf("Claim = %v, want a nil error (idle on a refusal, a payload otherwise)", err)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Fatalf("the claim made %d issue tracker requests", n)
			}
			if !tc.wantBlock {
				if payload == nil {
					t.Fatal("a healthy forge must clear the claim, got idle")
				}
				if f.fs.claimFailed != nil {
					t.Fatalf("a cleared claim must not fail the run, got %+v", f.fs.claimFailed)
				}
				return
			}
			if payload != nil {
				t.Fatalf("a refused claim must return no payload, got %+v", payload)
			}
			if f.fs.claimFailed == nil || f.fs.claimFailed.ID != f.runID {
				t.Fatalf("claimFailed = %+v, want the run failed", f.fs.claimFailed)
			}
			if reason := f.fs.claimFailed.FailureReason.String; !strings.Contains(reason, "default-branch guardrail") {
				t.Fatalf("failure reason = %q, want it to name the guardrail", reason)
			}
		})
	}
}
