package issuetracker

import (
	"context"
	"net/http"
	"testing"
)

func TestDeletedCommentsAreDropped(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("t", "")
	srv.SeedComment(n, "alice", "first")
	srv.SeedDeletedComment(n, "bob", "gone")
	srv.SeedComment(n, "carol", "second")
	got, err := y.ListIssueComments(context.Background(), 1, n)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Body != "first" || got[1].Body != "second" {
		t.Fatalf("comments = %+v, want first and second only", got)
	}
}

func TestCreateIssueNoteRefusesAMalformedCommentID(t *testing.T) {
	y, _ := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeRaw(w, `{"id":"../x","text":"n"}`)
			return
		}
		writeRaw(w, rawIssue("2-1", "UZI-1", 1, "0-1", ""))
	})
	if _, err := y.CreateIssueNote(context.Background(), 1, 1, "n"); err == nil {
		t.Fatal("CreateIssueNote accepted a malformed comment id")
	}
}
