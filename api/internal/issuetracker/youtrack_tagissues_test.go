package issuetracker

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

// listIIDs lists with opts and returns the issue numbers in the order returned.
func listIIDs(t *testing.T, y *youTrack, opts forge.ListIssuesOptions) []int64 {
	t.Helper()
	got, err := y.ListIssues(context.Background(), testProjectID, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := []int64{}
	for _, i := range got {
		out = append(out, i.IID)
	}
	return out
}

func tagListingPath(id string) string { return "/api/tags/" + id + "/issues" }

func TestListIssuesNeverSendsATagNameInAQuery(t *testing.T) {
	y, srv := newFakeDriver(t)
	own := srv.SeedOwnTag("mine")
	srv.SeedIssue("a", "", youtracktest.EligibilityTagID, own)
	srv.ResetRequests()
	for _, opts := range []forge.ListIssuesOptions{
		{},
		{State: forge.StateOpened},
		{Labels: []string{youtracktest.EligibilityTagName}},
		{Labels: []string{"mine"}},
		{Labels: []string{"mine", youtracktest.EligibilityTagName}},
	} {
		if _, err := y.ListIssues(context.Background(), testProjectID, opts); err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
	}
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request recorded")
	}
	for _, r := range reqs {
		q, err := url.ParseQuery(r.RawQuery)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(q.Get("query"), "tag:") || strings.Contains(r.RawQuery, "tag%3A") {
			t.Errorf("request %s %s carries a tag term in its query: %s", r.Method, r.Path, r.RawQuery)
		}
	}
}

func TestListIssuesByLabelDropsTaggedIssuesOfOtherProjects(t *testing.T) {
	y, srv := newFakeDriver(t)
	keep := srv.SeedIssue("here", "", youtracktest.EligibilityTagID)
	moved := srv.SeedIssue("moved", "", youtracktest.EligibilityTagID)
	srv.MoveIssue(moved, youtracktest.OtherProjectID)
	if got := listIIDs(t, y, forge.ListIssuesOptions{Labels: []string{youtracktest.EligibilityTagName}}); !slices.Equal(got, []int64{keep}) {
		t.Errorf("listed %v, want only %d (issue %d sits in another project)", got, keep, moved)
	}
}

func TestListIssuesOfARenamedEligibilityTagUsesTheStoredName(t *testing.T) {
	y, srv := newFakeDriver(t)
	n := srv.SeedIssue("eligible", "", youtracktest.EligibilityTagID)
	srv.SeedTagActivity(n, youtracktest.OtherLogin, []string{youtracktest.EligibilityTagID}, nil)
	srv.RenameTag(youtracktest.EligibilityTagID, "renamed-by-owner")
	srv.ResetRequests()
	got, err := y.ListIssues(context.Background(), testProjectID, forge.ListIssuesOptions{Labels: []string{youtracktest.EligibilityTagName}})
	if err != nil || len(got) != 1 || got[0].IID != n {
		t.Fatalf("ListIssues = %+v, %v; want issue %d", got, err, n)
	}
	if !slices.Equal(got[0].Labels, []string{youtracktest.EligibilityTagName}) {
		t.Errorf("labels = %v, want the stored name %q", got[0].Labels, youtracktest.EligibilityTagName)
	}
	for _, r := range srv.Requests() {
		if r.Path != tagListingPath(youtracktest.EligibilityTagID) {
			t.Errorf("sent %s %s; a renamed eligibility tag needs only its listing", r.Method, r.Path)
		}
	}
	read, err := y.GetIssue(context.Background(), testProjectID, n)
	if err != nil || !slices.Equal(read.Labels, []string{youtracktest.EligibilityTagName}) {
		t.Errorf("GetIssue labels = %v, %v; want the stored name", read.Labels, err)
	}
	events, err := y.ListIssueLabelEvents(context.Background(), testProjectID, n)
	if err != nil || len(events) != 1 || events[0].LabelName != youtracktest.EligibilityTagName {
		t.Errorf("events = %+v, %v; want one under the stored name", events, err)
	}
}

func TestListIssuesPagesTheTagListing(t *testing.T) {
	y, srv := newFakeDriver(t)
	const total = 2*pageSize + 30
	want := make([]int64, 0, total)
	for range total {
		want = append(want, srv.SeedIssue("t", "", youtracktest.EligibilityTagID))
	}
	srv.SeedIssue("untagged", "")
	srv.ResetRequests()
	got := listIIDs(t, y, forge.ListIssuesOptions{Labels: []string{youtracktest.EligibilityTagName}})
	if !slices.Equal(got, want) {
		t.Errorf("listed %d issues, want all %d in ascending update order", len(got), total)
	}
	if n := len(srv.Requests()); n != 3 {
		t.Errorf("%d requests, want 3 pages", n)
	}
}

func TestListIssuesByLabelSortsAscendingAndLimitsAfterTheWholeWalk(t *testing.T) {
	y, srv := newFakeDriver(t)
	var iids []int64
	for range pageSize + 20 {
		iids = append(iids, srv.SeedIssue("t", "", youtracktest.EligibilityTagID))
	}
	// The fake answers newest first, so the oldest issues are on the last page.
	srv.ResetRequests()
	got := listIIDs(t, y, forge.ListIssuesOptions{Labels: []string{youtracktest.EligibilityTagName}, Limit: 3})
	if !slices.Equal(got, iids[:3]) {
		t.Errorf("listed %v, want the three oldest %v", got, iids[:3])
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("%d requests, want both pages walked before the limit", n)
	}
}

func TestListIssuesByAnOwnedLabelListsThroughItsOwnTag(t *testing.T) {
	y, srv := newFakeDriver(t)
	own := srv.SeedOwnTag("mine")
	n := srv.SeedIssue("a", "", own)
	srv.SeedIssue("b", "", youtracktest.EligibilityTagID)
	srv.ResetRequests()
	if got := listIIDs(t, y, forge.ListIssuesOptions{Labels: []string{"mine"}}); !slices.Equal(got, []int64{n}) {
		t.Errorf("listed %v, want [%d]", got, n)
	}
	var listings []string
	for _, r := range srv.Requests() {
		if strings.HasSuffix(r.Path, "/issues") {
			listings = append(listings, r.Path)
		}
	}
	if !slices.Equal(listings, []string{tagListingPath(own)}) {
		t.Errorf("listings = %v, want only the owned tag's", listings)
	}
}

func TestListIssuesWithTwoLabelsWalksTheEligibilityTagAndRequiresBoth(t *testing.T) {
	y, srv := newFakeDriver(t)
	own := srv.SeedOwnTag("mine")
	both := srv.SeedIssue("both", "", own, youtracktest.EligibilityTagID)
	srv.SeedIssue("only eligible", "", youtracktest.EligibilityTagID)
	srv.SeedIssue("only mine", "", own)
	srv.ResetRequests()
	if got := listIIDs(t, y, forge.ListIssuesOptions{Labels: []string{"mine", youtracktest.EligibilityTagName}}); !slices.Equal(got, []int64{both}) {
		t.Errorf("listed %v, want [%d]", got, both)
	}
	for _, r := range srv.Requests() {
		if strings.HasSuffix(r.Path, "/issues") && r.Path != tagListingPath(youtracktest.EligibilityTagID) {
			t.Errorf("walked %s, want the eligibility tag's listing", r.Path)
		}
	}
}

func TestListLabelsNeedsTheAccount(t *testing.T) {
	var failMe atomic.Bool
	failMe.Store(true)
	y, _ := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users/me":
			if failMe.Load() {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			writeRaw(w, rawMe)
		default:
			writeRaw(w, `[{"id":"5-1","name":"dup","owner":{"id":"`+rawMeID+`"}}]`)
		}
	})
	if got, err := y.ListLabels(context.Background(), testProjectID); err == nil {
		t.Fatalf("ListLabels = %+v with users/me failing, want an error", got)
	}
	failMe.Store(false)
	got, err := y.ListLabels(context.Background(), testProjectID)
	if err != nil || len(got) != 2 {
		t.Errorf("ListLabels after the account lookup recovered = %+v, %v; want the eligibility name and dup", got, err)
	}
}

func TestListLabelsExcludesForeignTagsAndListsTheEligibilityNameOnce(t *testing.T) {
	y, srv := newFakeDriver(t)
	everyone := youtracktest.Sharing{Groups: []string{youtracktest.AllUsersGroupID}}
	srv.SeedTag("theirs", youtracktest.OtherLogin, everyone, youtracktest.Sharing{})
	srv.SeedTag(youtracktest.EligibilityTagName, youtracktest.OtherLogin, everyone, youtracktest.Sharing{})
	srv.SeedOwnTag("mine")
	srv.SeedOwnTag("mine")
	got, err := y.ListLabels(context.Background(), testProjectID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range got {
		names = append(names, l.Name)
	}
	if want := []string{youtracktest.EligibilityTagName, "mine"}; !slices.Equal(names, want) {
		t.Errorf("ListLabels = %v, want %v", names, want)
	}
}

func TestMapsAuthor(t *testing.T) {
	ctx := context.Background()
	var failMe atomic.Bool
	var meCalls atomic.Int32
	y, _ := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/me" {
			http.NotFound(w, r)
			return
		}
		meCalls.Add(1)
		if failMe.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		writeRaw(w, rawMe)
	})
	failMe.Store(true)
	if ok, err := y.MapsAuthor(ctx, ""); ok || err != nil {
		t.Errorf("empty login = %v, %v; want false, nil", ok, err)
	}
	if n := meCalls.Load(); n != 0 {
		t.Errorf("an empty login asked users/me %d times, want 0", n)
	}
	if ok, err := y.MapsAuthor(ctx, "u"); ok || err == nil {
		t.Errorf("users/me failing = %v, %v; want false and an error", ok, err)
	}
	failMe.Store(false)
	if ok, err := y.MapsAuthor(ctx, "u"); !ok || err != nil {
		t.Errorf("matching login after a failure = %v, %v; want true, nil (the failure is not kept)", ok, err)
	}
	if ok, err := y.MapsAuthor(ctx, "someone-else"); ok || err != nil {
		t.Errorf("other login = %v, %v; want false, nil", ok, err)
	}
	if ok, err := y.MapsAuthor(ctx, "U"); ok || err != nil {
		t.Errorf("different case = %v, %v; want false, nil", ok, err)
	}
}

func TestMapsAuthorNeverMapsAnEmptyAccountLogin(t *testing.T) {
	y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"id":"`+rawMeID+`","login":""}`)
	})
	if ok, err := y.MapsAuthor(context.Background(), "x"); ok || err != nil {
		t.Errorf("account with no login = %v, %v; want false, nil", ok, err)
	}
}
