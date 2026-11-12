package issuetracker

import (
	"context"
	"net/url"
)

// IssueTextMapped reports whether every recorded change to the issue's title
// and body, its creation included, was made by the connection's own account:
// D11 of the external issue trackers PRD for the issue text. The reporter alone
// does not say who wrote the text, because another user can edit a description
// the account reported. An empty or different author login on any activity, or
// a failed account lookup, means not mapped; a failed read is an error and the
// caller treats it as unknown. The consumer declares the interface
// (issueinput.IssueTextMapper); this assertion only guards the method.
var _ interface {
	IssueTextMapped(ctx context.Context, projectID, issueIID int64) (bool, error)
} = (*youTrack)(nil)

func (y *youTrack) IssueTextMapped(ctx context.Context, projectID, issueIID int64) (bool, error) {
	w, err := y.resolveIssue(ctx, projectID, issueIID)
	if err != nil {
		return false, err
	}
	a, err := y.connAccount(ctx)
	if err != nil {
		return false, err
	}
	acts, err := ytList[ytTextActivity](ctx, y, []string{"issues", w.ID, "activities"}, url.Values{
		"categories": {"IssueCreatedCategory,SummaryCategory,DescriptionCategory"},
		"fields":     {"id,timestamp,author(id,login)"},
	})
	if err != nil {
		return false, err
	}
	for _, act := range acts {
		if l := login(act.Author); l == "" || l != a.Login {
			return false, nil
		}
	}
	return a.Login != "", nil
}
