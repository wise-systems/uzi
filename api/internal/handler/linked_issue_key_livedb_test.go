package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

// wantKey is the key a linked card or detail carries: the stored project short name, a
// dash and the issue number.
func wantKey(n int64) string {
	return youtracktest.ProjectShortName + "-" + strconv.FormatInt(n, 10)
}

// Promote on a linked repo for an untagged issue adds the stored eligibility tag, and the
// response card carries the derived issue key even though the cached row (a create path's
// write) has no issue_key of its own.
func TestLinkedRepoPromoteCardCarriesTheIssueKeyLiveDB(t *testing.T) {
	f := newLinkedHandlerFixture(t, true)
	untagged := f.srv.SeedIssue("untagged card", "body")
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO issues (repo_id, forge_issue_iid, title, state, labels, web_url, forge_updated_at, synced_at)
	      VALUES ($1, $2, 'untagged', 'opened', '[]', 'https://yt.test/u', now(), now())`, f.repo, untagged); err != nil {
		t.Fatal(err)
	}
	f.issueN = untagged
	rr := f.call(f.h.PromoteIssue, http.MethodPost, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	wantTags(t, f.srv, untagged, true, youtracktest.EligibilityTagName)
	var resp struct {
		Card struct {
			IssueKey string `json:"issue_key"`
		} `json:"card"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Card.IssueKey != wantKey(untagged) {
		t.Errorf("promoted card issue_key = %q (%v), want %q", resp.Card.IssueKey, err, wantKey(untagged))
	}
}

type detailResp struct {
	Issue struct {
		IssueKey         string `json:"issue_key"`
		EligibilityLabel string `json:"eligibility_label"`
		Column           string `json:"column"`
		Closed           bool   `json:"closed"`
	} `json:"issue"`
}

func (f *linkedHandlerFixture) detail(t *testing.T) detailResp {
	t.Helper()
	rr := f.call(f.h.GetIssueDetail, http.MethodGet, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var out detailResp
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The issue page of a linked repo carries the key and the stored eligibility name, and its
// column is the cached State column, not a label.
func TestLinkedRepoIssueDetailCarriesKeyEligibilityAndCachedColumnLiveDB(t *testing.T) {
	f := newLinkedHandlerFixture(t, true)
	if _, err := f.pool.Exec(context.Background(), `UPDATE issues SET issue_tracker_column = 'Human Review' WHERE repo_id = $1 AND forge_issue_iid = $2`, f.repo, f.issueN); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	if d.Issue.IssueKey != wantKey(f.issueN) || d.Issue.EligibilityLabel != youtracktest.EligibilityTagName || d.Issue.Column != "Human Review" {
		t.Errorf("detail = %+v, want key %q, eligibility %q, column Human Review", d.Issue, wantKey(f.issueN), youtracktest.EligibilityTagName)
	}
}

// An issue the cache does not hold shows in the Open lane even when a tag it carries is
// named like a column: labels never decide the column on a linked repo.
func TestLinkedRepoUncachedIssueDetailIsOpenEvenWithAColumnNamedTagLiveDB(t *testing.T) {
	f := newLinkedHandlerFixture(t, true)
	tagID := f.srv.SeedOwnTag("In Progress")
	n := f.srv.SeedIssue("uncached", "body", youtracktest.EligibilityTagID, tagID)
	f.issueN = n
	d := f.detail(t)
	if d.Issue.Column != "" || d.Issue.Closed || d.Issue.IssueKey != wantKey(n) {
		t.Errorf("detail = %+v, want the Open lane and key %q", d.Issue, wantKey(n))
	}
}

// A linked board carries the stored eligibility name, and every card carries the derived
// key, including a cached row whose issue_key column is NULL.
func TestLinkedRepoBoardCardsCarryTheDerivedKeyAndEligibilityLabelLiveDB(t *testing.T) {
	f := newLinkedHandlerFixture(t, true)
	rr := f.call(f.h.GetBoard, http.MethodGet, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("board = %d\n%s", rr.Code, rr.Body.String())
	}
	var b struct {
		Board struct {
			EligibilityLabel string `json:"eligibility_label"`
			Cards            []struct {
				IID      int64  `json:"iid"`
				IssueKey string `json:"issue_key"`
			} `json:"cards"`
		} `json:"board"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Board.EligibilityLabel != youtracktest.EligibilityTagName {
		t.Errorf("eligibility_label = %q", b.Board.EligibilityLabel)
	}
	for _, c := range b.Board.Cards {
		if c.IssueKey != wantKey(c.IID) {
			t.Errorf("card %d issue_key = %q, want %q", c.IID, c.IssueKey, wantKey(c.IID))
		}
	}
	if len(b.Board.Cards) == 0 {
		t.Fatal("the board has no cards")
	}
}
