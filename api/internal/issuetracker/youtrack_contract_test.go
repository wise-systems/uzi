package issuetracker_test

import (
	"testing"

	"github.com/vtmocanu/uzi/api/internal/issuetracker"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/issuetrackertest"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

// TestYouTrackContract runs the shared issue tracker contract against the
// YouTrack driver over the in-process fake.
func TestYouTrackContract(t *testing.T) {
	issuetrackertest.RunContract(t, func(t *testing.T) issuetrackertest.Subject {
		srv := youtracktest.NewServer(t)
		y, err := issuetracker.NewYouTrackOver(issuetracker.Options{
			BaseURL:     srv.URL,
			Token:       srv.Token,
			Project:     issuetracker.ProjectRef{ID: youtracktest.ProjectID, ShortName: youtracktest.ProjectShortName},
			OpenState:   "Open",
			ClosedState: "Fixed",
		}, srv.Client().Transport)
		if err != nil {
			t.Fatal(err)
		}
		return issuetrackertest.Subject{
			IssueTracker: y,
			ProjectID:    1,
			SeedHumanComment: func(t *testing.T, iid int64, username, body string) {
				srv.SeedComment(iid, username, body)
			},
			SeedLabelEvent: func(t *testing.T, iid int64, action, label, username string) {
				if action == "add" {
					srv.SeedTagActivity(iid, username, []string{label}, nil)
				} else {
					srv.SeedTagActivity(iid, username, nil, []string{label})
				}
			},
			MoveIssue: func(t *testing.T, iid int64) {
				srv.MoveIssue(iid, youtracktest.OtherProjectID)
			},
		}
	})
}
