package issuetracker

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// ListIssueComments implements forge.IssueTracker: deleted comments are
// dropped and the rest come back oldest first. Author ids are 0 (unresolved),
// because YouTrack account ids are strings.
func (y *youTrack) ListIssueComments(ctx context.Context, _, issueIID int64) ([]forge.IssueComment, error) {
	w, err := y.resolveIssue(ctx, issueIID)
	if err != nil {
		return nil, err
	}
	comments, err := ytList[ytComment](ctx, y, []string{"issues", w.ID, "comments"}, url.Values{"fields": {commentFields}})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(comments, func(a, b ytComment) int {
		switch {
		case a.Created < b.Created:
			return -1
		case a.Created > b.Created:
			return 1
		}
		return 0
	})
	out := []forge.IssueComment{}
	for _, c := range comments {
		if c.Deleted {
			continue
		}
		out = append(out, forge.IssueComment{
			AuthorUsername: login(c.Author),
			Body:           c.Text,
			CreatedAt:      time.UnixMilli(c.Created).UTC(),
		})
	}
	return out, nil
}

// CreateIssueNote implements forge.IssueTracker. ID is the numeric suffix of
// the comment's entity id.
func (y *youTrack) CreateIssueNote(ctx context.Context, _, issueIID int64, body string) (forge.IssueNote, error) {
	w, err := y.resolveIssue(ctx, issueIID)
	if err != nil {
		return forge.IssueNote{}, err
	}
	var c ytComment
	_, _, err = y.do(ctx, ytCall{
		method: http.MethodPost,
		path:   []string{"issues", w.ID, "comments"},
		query:  url.Values{"fields": {"id,text,created,author(id,login)"}},
		body:   map[string]string{"text": body},
		limit:  objectBytes,
		prov:   forge.ResponseSuccess,
	}, &c)
	if err != nil {
		return forge.IssueNote{}, err
	}
	id, err := noteID(c.ID)
	if err != nil {
		return forge.IssueNote{}, y.red.error(err)
	}
	return forge.IssueNote{ID: id, Body: c.Text}, nil
}
