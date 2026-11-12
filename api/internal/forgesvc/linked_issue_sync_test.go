package forgesvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

func TestSyncIssuesDispatchesOnTheLink(t *testing.T) {
	marks := Marks{PRD: time.Unix(100, 0)}

	t.Run("linked: no forge call, typed error, marks kept", func(t *testing.T) {
		f := &fakeForge{}
		guarded := &issueWriteGuard{inner: f}
		s := newTestService(&fakeStore{})
		if _, err := s.SyncIssues(context.Background(), uuid.New(), testProjectID, guarded); !errors.Is(err, ErrLinkedSyncNotReady) {
			t.Fatalf("SyncIssues error = %v, want ErrLinkedSyncNotReady", err)
		}
		got, err := s.SyncIssuesIncremental(context.Background(), uuid.New(), testProjectID, guarded, marks)
		if !errors.Is(err, ErrLinkedSyncNotReady) || got != marks {
			t.Fatalf("SyncIssuesIncremental = (%+v, %v)", got, err)
		}
		if len(f.listCalls) != 0 {
			t.Fatalf("a linked sync reached the forge: %+v", f.listCalls)
		}
	})

	t.Run("unlinked: the forge sync, same calls", func(t *testing.T) {
		viaDispatcher := &fakeForge{issues: []forge.Issue{issueAt(1, recordedMarkBase)}}
		direct := &fakeForge{issues: []forge.Issue{issueAt(1, recordedMarkBase)}}
		if _, err := newTestService(&fakeStore{}).SyncIssues(context.Background(), uuid.New(), testProjectID, viaDispatcher); err != nil {
			t.Fatal(err)
		}
		if _, err := newTestService(&fakeStore{}).FullSync(context.Background(), uuid.New(), testProjectID, direct); err != nil {
			t.Fatal(err)
		}
		if len(viaDispatcher.listCalls) == 0 || len(viaDispatcher.listCalls) != len(direct.listCalls) {
			t.Fatalf("dispatcher made %d list calls, direct FullSync %d", len(viaDispatcher.listCalls), len(direct.listCalls))
		}
	})
}
