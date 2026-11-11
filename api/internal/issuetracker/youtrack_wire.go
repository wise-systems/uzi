package issuetracker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// fields parameters. YouTrack returns only what is asked for.
const (
	issueFields    = "id,idReadable,numberInProject,summary,description,created,updated,resolved,reporter(id,login),tags(id,name),project(id,shortName)"
	tagFields      = "id,name"
	commentFields  = "id,text,created,deleted,author(id,login)"
	activityFields = "id,timestamp,author(id,login),added(id,name),removed(id,name)"
)

// idReadableRE matches an id-readable such as "UZI-15"; it ends up in WebURL.
var idReadableRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

type ytUser struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

type ytTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ytProject struct {
	ID        string `json:"id"`
	ShortName string `json:"shortName"`
	Name      string `json:"name"`
}

type ytIssue struct {
	ID              string     `json:"id"`
	IDReadable      string     `json:"idReadable"`
	NumberInProject int64      `json:"numberInProject"`
	Summary         string     `json:"summary"`
	Description     string     `json:"description"`
	Created         int64      `json:"created"`
	Updated         int64      `json:"updated"`
	Resolved        *int64     `json:"resolved"`
	Reporter        *ytUser    `json:"reporter"`
	Tags            []ytTag    `json:"tags"`
	Project         *ytProject `json:"project"`
}

type ytComment struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Created int64   `json:"created"`
	Deleted bool    `json:"deleted"`
	Author  *ytUser `json:"author"`
}

type ytActivity struct {
	ID        string  `json:"id"`
	Timestamp int64   `json:"timestamp"`
	Author    *ytUser `json:"author"`
	Added     []ytTag `json:"added"`
	Removed   []ytTag `json:"removed"`
}

// toIssue maps a wire issue to the neutral type. The issue must carry a valid
// entity id and id-readable.
func (y *youTrack) toIssue(w ytIssue) (forge.Issue, error) {
	if !entityIDRE.MatchString(w.ID) || !idReadableRE.MatchString(w.IDReadable) {
		return forge.Issue{}, y.fail("youtrack: issue with a malformed id in the response")
	}
	issue := forge.Issue{
		IID:         w.NumberInProject,
		Title:       w.Summary,
		State:       string(forge.StateOpened),
		Labels:      make([]string, 0, len(w.Tags)),
		Description: w.Description,
		WebURL:      y.baseURL + "/issue/" + w.IDReadable,
		UpdatedAt:   time.UnixMilli(w.Updated).UTC(),
		Assignees:   []int64{},
	}
	if w.Resolved != nil {
		issue.State = string(forge.StateClosed)
	}
	if w.Reporter != nil {
		issue.Author = w.Reporter.Login
	}
	for _, t := range w.Tags {
		issue.Labels = append(issue.Labels, t.Name)
	}
	return issue, nil
}

// inBoundProject reports whether the issue sits in the bound project.
func (y *youTrack) inBoundProject(w ytIssue) bool {
	return w.Project != nil && w.Project.ID == y.project.ID && w.NumberInProject > 0
}

// noteID is the numeric suffix of a comment entity id such as "4-17".
func noteID(id string) (int64, error) {
	if !entityIDRE.MatchString(id) {
		return 0, fmt.Errorf("youtrack: comment with a malformed id in the response")
	}
	n, err := strconv.ParseInt(id[strings.IndexByte(id, '-')+1:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("youtrack: comment with a malformed id in the response")
	}
	return n, nil
}

func login(u *ytUser) string {
	if u == nil {
		return ""
	}
	return u.Login
}
