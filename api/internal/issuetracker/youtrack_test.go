package issuetracker

import (
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/issuetracker/youtracktest"
)

// fakeOptions are the Options that bind a driver to the fake's main project
// with the state mapping its State bundle supports.
func fakeOptions(srv *youtracktest.Server) Options {
	return Options{
		BaseURL:     srv.URL,
		Token:       srv.Token,
		Project:     ProjectRef{ID: youtracktest.ProjectID, ShortName: youtracktest.ProjectShortName},
		OpenState:   "Open",
		ClosedState: "Fixed",
	}
}

// newFakeDriver starts the fake and builds a driver over it.
func newFakeDriver(t *testing.T) (*youTrack, *youtracktest.Server) {
	t.Helper()
	srv := youtracktest.NewServer(t)
	y, err := newYouTrackOver(fakeOptions(srv), srv.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return y, srv
}

// credential assembles a token-shaped value at runtime, with characters that
// each escaping form rewrites.
func credential() string {
	return strings.Join([]string{"perm", ":", "ab c/d\"e", "-", "Zq9xLm2VnB7wKp3T"}, "")
}

// youtracktestBot is the login the fake's bearer token authenticates as.
const youtracktestBot = youtracktest.BotLogin

// newFakeDriverWithField is newFakeDriver with the State field renamed and the
// driver told the new name.
func newFakeDriverWithField(t *testing.T, field string) (*youtracktest.Server, *youTrack) {
	t.Helper()
	srv := youtracktest.NewServer(t)
	srv.SetStateField(field)
	opts := fakeOptions(srv)
	opts.StateField = field
	y, err := newYouTrackOver(opts, srv.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return srv, y
}
