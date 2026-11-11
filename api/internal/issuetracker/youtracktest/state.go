package youtracktest

import (
	"fmt"
	"strings"
	"time"
)

// Fixed identities of the fake's data.
const (
	// ProjectID and ProjectShortName name the project issues are created in.
	ProjectID        = "0-1"
	ProjectShortName = "UZI"
	// OtherProjectID and OtherProjectShortName name a second project, the
	// target of MoveIssue.
	OtherProjectID        = "0-2"
	OtherProjectShortName = "OTH"
	// BotLogin is the account the bearer token authenticates as.
	BotLogin = "uzi-bot"
	botID    = "1-1"
	// DefaultStateField is the name of the State custom field.
	DefaultStateField = "State"
)

type project struct {
	id, shortName, name string
	next                int64 // last number handed out in this project
}

type tag struct {
	id, name string
}

type comment struct {
	id      string
	text    string
	created int64
	deleted bool
	author  string
}

type activity struct {
	id        string
	timestamp int64
	author    string
	added     []string // tag ids
	removed   []string
}

type stateValue struct {
	id, name string
	resolved bool
	ordinal  int
}

type issue struct {
	id          string
	project     *project
	number      int64
	summary     string
	description string
	created     int64
	updated     int64
	resolved    *int64
	reporter    string
	tags        []string // tag ids, oldest first
	comments    []*comment
	activities  []*activity
}

func (i *issue) readable() string { return fmt.Sprintf("%s-%d", i.project.shortName, i.number) }

// model is the in-memory YouTrack state. Server.mu guards every field.
type model struct {
	projects   []*project
	tags       []*tag
	issues     []*issue
	aliases    map[string]*issue // every id-readable an issue has answered to
	states     []stateValue
	stateField string
	clock      int64
	seq        int64
}

func newModel() *model {
	return &model{
		projects: []*project{
			{id: ProjectID, shortName: ProjectShortName, name: "Uzi"},
			{id: OtherProjectID, shortName: OtherProjectShortName, name: "Other"},
		},
		aliases: map[string]*issue{},
		states: []stateValue{
			{id: "10-1", name: "Open", resolved: false, ordinal: 0},
			{id: "10-2", name: "Fixed", resolved: true, ordinal: 1},
			{id: "10-3", name: "Won't fix", resolved: true, ordinal: 2},
		},
		stateField: DefaultStateField,
		clock:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

// now returns a strictly increasing epoch-millisecond clock, one second per
// call, so ordering assertions never depend on wall time.
func (m *model) now() int64 {
	m.clock += 1000
	return m.clock
}

func (m *model) nextID(prefix int) string {
	m.seq++
	return fmt.Sprintf("%d-%d", prefix, m.seq)
}

func (m *model) projectByID(id string) *project {
	for _, p := range m.projects {
		if p.id == id {
			return p
		}
	}
	return nil
}

func (m *model) projectByShort(short string) *project {
	for _, p := range m.projects {
		if strings.EqualFold(p.shortName, short) {
			return p
		}
	}
	return nil
}

func (m *model) tagByID(id string) *tag {
	for _, t := range m.tags {
		if t.id == id {
			return t
		}
	}
	return nil
}

// tagByName returns the tag called name, creating it when missing.
func (m *model) ensureTag(name string) *tag {
	for _, t := range m.tags {
		if t.name == name {
			return t
		}
	}
	t := &tag{id: m.nextID(5), name: name}
	m.tags = append(m.tags, t)
	return t
}

func (m *model) newIssue(p *project, summary, description string) *issue {
	p.next++
	ts := m.now()
	i := &issue{
		id: m.nextID(2), project: p, number: p.next, summary: summary, description: description,
		created: ts, updated: ts, reporter: BotLogin,
	}
	m.issues = append(m.issues, i)
	m.aliases[i.readable()] = i
	return i
}

// lookup resolves an entity id or any id-readable the issue has had.
func (m *model) lookup(id string) *issue {
	for _, i := range m.issues {
		if i.id == id {
			return i
		}
	}
	return m.aliases[id]
}

func (m *model) byNumber(n int64) *issue {
	return m.aliases[fmt.Sprintf("%s-%d", ProjectShortName, n)]
}

func (i *issue) hasTag(id string) bool {
	for _, t := range i.tags {
		if t == id {
			return true
		}
	}
	return false
}

// addTag adds a tag and records the activity, unless it is already present.
func (m *model) addTag(i *issue, t *tag, author string) {
	if i.hasTag(t.id) {
		return
	}
	ts := m.now()
	i.tags = append(i.tags, t.id)
	i.updated = ts
	i.activities = append(i.activities, &activity{id: m.nextID(3), timestamp: ts, author: author, added: []string{t.id}})
}

func (m *model) removeTag(i *issue, t *tag, author string) {
	if !i.hasTag(t.id) {
		return
	}
	ts := m.now()
	kept := i.tags[:0:0]
	for _, id := range i.tags {
		if id != t.id {
			kept = append(kept, id)
		}
	}
	i.tags = kept
	i.updated = ts
	i.activities = append(i.activities, &activity{id: m.nextID(3), timestamp: ts, author: author, removed: []string{t.id}})
}

func (m *model) stateByName(name string) *stateValue {
	for k := range m.states {
		if m.states[k].name == name {
			return &m.states[k]
		}
	}
	return nil
}

// issueJSON is the full wire object of an issue; callers project it through a
// fields spec.
func (m *model) issueJSON(i *issue) map[string]any {
	tags := make([]map[string]any, 0, len(i.tags))
	for _, id := range i.tags {
		if t := m.tagByID(id); t != nil {
			tags = append(tags, map[string]any{"id": t.id, "name": t.name, "$type": "Tag"})
		}
	}
	var resolved any
	if i.resolved != nil {
		resolved = *i.resolved
	}
	return map[string]any{
		"$type":           "Issue",
		"id":              i.id,
		"idReadable":      i.readable(),
		"numberInProject": i.number,
		"summary":         i.summary,
		"description":     i.description,
		"created":         i.created,
		"updated":         i.updated,
		"resolved":        resolved,
		"reporter":        userJSON(i.reporter),
		"tags":            tags,
		"project":         map[string]any{"id": i.project.id, "shortName": i.project.shortName, "$type": "Project"},
	}
}

func userJSON(login string) map[string]any {
	return map[string]any{"$type": "User", "id": botID, "login": login}
}

func (m *model) tagsJSON(ids []string) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		if t := m.tagByID(id); t != nil {
			out = append(out, map[string]any{"id": t.id, "name": t.name, "$type": "Tag"})
		}
	}
	return out
}

// parseDay returns the epoch milliseconds of the start of the UTC day.
func parseDay(s string) (int64, error) {
	t, err := time.Parse("2006-01-02", s)
	return t.UnixMilli(), err
}
