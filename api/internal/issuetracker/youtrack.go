package issuetracker

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// defaultStateField is the YouTrack custom field that holds the issue state.
const defaultStateField = "State"

var (
	shortNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	// entityIDRE matches a YouTrack entity id such as "2-15". Every id taken
	// from a response and placed in a path is checked against it.
	entityIDRE = regexp.MustCompile(`^\d+-\d+$`)
)

// youTrack is the JetBrains YouTrack driver. It is bound to one project when
// built (see ProjectRef): the projectID argument of every forge.IssueTracker
// method is ignored, and a driver with no project bound refuses them with
// ErrProjectNotBound. VerifyToken, ListProjects and ListStates need no binding.
type youTrack struct {
	baseURL     string // scheme and host, no trailing slash
	token       string
	project     ProjectRef
	stateField  string
	openState   string
	closedState string
	client      *http.Client
	red         redactor
}

var _ Driver = (*youTrack)(nil)

// newYouTrack is the registry constructor: system roots, default transport.
func newYouTrack(opts Options) (Driver, error) {
	return buildYouTrack(opts, nil)
}

// buildYouTrack validates opts and builds the driver over base, which is nil
// outside tests.
func buildYouTrack(opts Options, base http.RoundTripper) (*youTrack, error) {
	baseURL, err := strictBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	if err := validateToken(opts.Token); err != nil {
		return nil, err
	}
	if opts.Project != (ProjectRef{}) {
		if !shortNameRE.MatchString(opts.Project.ShortName) {
			return nil, errors.New("issuetracker: invalid project short name")
		}
		if !entityIDRE.MatchString(opts.Project.ID) {
			return nil, errors.New("issuetracker: invalid project id")
		}
	}
	stateField := opts.StateField
	if stateField == "" {
		stateField = defaultStateField
	}
	return &youTrack{
		baseURL:     baseURL,
		token:       opts.Token,
		project:     opts.Project,
		stateField:  stateField,
		openState:   opts.OpenState,
		closedState: opts.ClosedState,
		client:      newHTTPClient(opts.Timeout, base),
		red:         newRedactor(opts.Token),
	}, nil
}

// strictBaseURL accepts only https://host[:port] with an optional single
// trailing slash. A path, userinfo, query or fragment is rejected rather than
// stripped, so the URL the token is sent to is exactly the one that was
// validated.
func strictBaseURL(raw string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("issuetracker: base URL %s", why)
	}
	if strings.ContainsAny(raw, "?#") {
		return bad("must not carry a query or fragment")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return bad("is not a valid URL")
	}
	switch {
	case u.Scheme != "https":
		return bad("must use https")
	case u.Opaque != "" || u.Hostname() == "":
		return bad("must name a host")
	case u.User != nil:
		return bad("must not carry credentials")
	case u.Path != "" && u.Path != "/":
		return bad("must not carry a path")
	}
	return "https://" + u.Host, nil
}

// requireProject returns the bound project or the ErrProjectNotBound sentinel.
func (y *youTrack) requireProject() (ProjectRef, error) {
	if y.project == (ProjectRef{}) {
		return ProjectRef{}, y.red.sentinel(ErrProjectNotBound, 0, "")
	}
	return y.project, nil
}

// fail returns a redacted error built from a format.
func (y *youTrack) fail(format string, args ...any) error {
	return y.red.error(fmt.Errorf(format, args...))
}
