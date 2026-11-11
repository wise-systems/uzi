package youtracktest

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// defaultTop is YouTrack's page size when $top is absent, so a driver that
// forgets to page sees a truncated list here as it would there.
const defaultTop = 42

// routes builds the mux. Every modelled path dispatches on the method inside,
// so a known path with an unmodelled method is also loud (501), not 405.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	route := func(pattern string, handlers map[string]http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if h, ok := handlers[r.Method]; ok {
				h(w, r)
				return
			}
			s.unmodelledRoute(w, r)
		})
	}
	route("/api/users/me", map[string]http.HandlerFunc{http.MethodGet: s.getMe})
	route("/api/admin/projects", map[string]http.HandlerFunc{http.MethodGet: s.listProjects})
	route("/api/admin/projects/{pid}/customFields", map[string]http.HandlerFunc{http.MethodGet: s.listProjectFields})
	route("/api/admin/projects/{pid}/customFields/{cf}/bundle/values", map[string]http.HandlerFunc{http.MethodGet: s.listBundleValues})
	route("/api/tags", map[string]http.HandlerFunc{http.MethodGet: s.listTags, http.MethodPost: s.createTag})
	route("/api/issues", map[string]http.HandlerFunc{http.MethodGet: s.listIssues, http.MethodPost: s.createIssue})
	route("/api/issues/{id}", map[string]http.HandlerFunc{http.MethodGet: s.getIssue, http.MethodPost: s.updateIssue})
	route("/api/issues/{id}/tags", map[string]http.HandlerFunc{http.MethodPost: s.addIssueTag})
	route("/api/issues/{id}/tags/{tag}", map[string]http.HandlerFunc{http.MethodDelete: s.removeIssueTag})
	route("/api/issues/{id}/activities", map[string]http.HandlerFunc{http.MethodGet: s.listActivities})
	route("/api/issues/{id}/comments", map[string]http.HandlerFunc{http.MethodGet: s.listComments, http.MethodPost: s.createComment})
	mux.HandleFunc("/", s.unmodelledRoute)
	return mux
}

// fields parses the mandatory fields parameter; it answers 400 and reports
// false when it is missing or malformed.
func fields(w http.ResponseWriter, r *http.Request) (fieldSpec, bool) {
	spec, ok := parseFields(r.URL.Query().Get("fields"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Bad Request", "a valid fields parameter is required")
	}
	return spec, ok
}

// page applies $top and $skip to items.
func page[T any](r *http.Request, items []T) []T {
	top, skip := defaultTop, 0
	if v := r.URL.Query().Get("$top"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			top = n
		}
	}
	if v := r.URL.Query().Get("$skip"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			skip = n
		}
	}
	if skip > len(items) {
		skip = len(items)
	}
	items = items[skip:]
	if top < len(items) {
		items = items[:top]
	}
	return items
}

// writePage projects, pages and writes a list. The items are projected after
// paging so a driver sees exactly the requested window.
func writePage(w http.ResponseWriter, r *http.Request, spec fieldSpec, items []map[string]any) {
	window := page(r, items)
	out := make([]any, 0, len(window))
	for _, it := range window {
		out = append(out, spec.project(it))
	}
	writeJSON(w, http.StatusOK, out)
}

func notFound(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, "Not Found", "Entity not found: "+what)
}

// decodeBody reads a strict JSON body; unknown members are a 400, so a driver
// cannot send fields the API ignores.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid request body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, spec.project(userJSON(BotLogin)))
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	var items []map[string]any
	for _, p := range s.m.projects {
		items = append(items, map[string]any{"$type": "Project", "id": p.id, "shortName": p.shortName, "name": p.name})
	}
	writePage(w, r, spec, items)
}

// stateFieldID is the id of the State custom field of project pid.
func stateFieldID(pid string) string { return "60-" + strings.TrimPrefix(pid, "0-") }

func (s *Server) listProjectFields(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	pid := r.PathValue("pid")
	if s.m.projectByID(pid) == nil {
		notFound(w, pid)
		return
	}
	items := []map[string]any{
		{"$type": "EnumProjectCustomField", "id": "61-" + strings.TrimPrefix(pid, "0-"), "field": map[string]any{"name": "Priority"}, "bundle": map[string]any{"id": "41-1"}},
		{"$type": "StateProjectCustomField", "id": stateFieldID(pid), "field": map[string]any{"name": s.m.stateField}, "bundle": map[string]any{"id": "40-1"}},
	}
	writePage(w, r, spec, items)
}

func (s *Server) listBundleValues(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	pid, cf := r.PathValue("pid"), r.PathValue("cf")
	if s.m.projectByID(pid) == nil || cf != stateFieldID(pid) {
		notFound(w, pid+"/"+cf)
		return
	}
	var items []map[string]any
	for _, v := range s.m.states {
		items = append(items, map[string]any{"$type": "StateBundleElement", "id": v.id, "name": v.name, "isResolved": v.resolved, "ordinal": v.ordinal})
	}
	writePage(w, r, spec, items)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	writePage(w, r, spec, s.m.tagsJSON(tagIDs(s.m.tags)))
}

func tagIDs(tags []*tag) []string {
	ids := make([]string, 0, len(tags))
	for _, t := range tags {
		ids = append(ids, t.id)
	}
	return ids
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "tag name is required")
		return
	}
	for _, t := range s.m.tags {
		if t.name == body.Name {
			writeError(w, http.StatusConflict, "Conflict", "tag already exists")
			return
		}
	}
	t := s.m.ensureTag(body.Name)
	writeJSON(w, http.StatusOK, spec.project(s.m.tagsJSON([]string{t.id})[0]))
}

var (
	queryProject    = regexp.MustCompile(`project: \{([^{}]*)\}`)
	queryTag        = regexp.MustCompile(`tag: \{([^{}]*)\}`)
	queryUpdated    = regexp.MustCompile(`updated: (\d{4}-\d{2}-\d{2}) \.\. \*`)
	queryUnresolved = regexp.MustCompile(`#Unresolved`)
	queryResolved   = regexp.MustCompile(`#Resolved`)
	querySort       = regexp.MustCompile(`sort by: updated asc`)
)

// listIssues understands exactly the query grammar the driver builds and
// answers 400 for anything else.
func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("query")
	var tagNames []string
	for _, m := range queryTag.FindAllStringSubmatch(q, -1) {
		tagNames = append(tagNames, m[1])
	}
	q = queryTag.ReplaceAllString(q, "")
	proj := queryProject.FindStringSubmatch(q)
	q = queryProject.ReplaceAllString(q, "")
	upd := queryUpdated.FindStringSubmatch(q)
	q = queryUpdated.ReplaceAllString(q, "")
	unresolved, resolved := queryUnresolved.MatchString(q), queryResolved.MatchString(q)
	q = queryResolved.ReplaceAllString(queryUnresolved.ReplaceAllString(querySort.ReplaceAllString(q, ""), ""), "")
	if strings.TrimSpace(q) != "" || proj == nil || (unresolved && resolved) {
		writeError(w, http.StatusBadRequest, "Bad Request", "unsupported query")
		return
	}
	p := s.m.projectByShort(proj[1])
	var floor int64
	if upd != nil {
		d, err := parseDay(upd[1])
		if err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "bad date")
			return
		}
		floor = d
	}
	var matched []*issue
	for _, i := range s.m.issues {
		if i.project != p || i.updated < floor || (unresolved && i.resolved != nil) || (resolved && i.resolved == nil) {
			continue
		}
		if !s.hasAllTags(i, tagNames) {
			continue
		}
		matched = append(matched, i)
	}
	slices.SortStableFunc(matched, func(a, b *issue) int { return int(a.updated - b.updated) })
	items := make([]map[string]any, 0, len(matched))
	for _, i := range matched {
		items = append(items, s.m.issueJSON(i))
	}
	writePage(w, r, spec, items)
}

// hasAllTags matches tag names case-insensitively, as YouTrack's query does;
// the driver filters again on the exact name.
func (s *Server) hasAllTags(i *issue, names []string) bool {
	for _, name := range names {
		found := false
		for _, id := range i.tags {
			if t := s.m.tagByID(id); t != nil && strings.EqualFold(t.name, name) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	var body struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	p := s.m.projectByID(body.Project.ID)
	if p == nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "unknown project")
		return
	}
	if strings.TrimSpace(body.Summary) == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "summary is required")
		return
	}
	i := s.m.newIssue(p, body.Summary, body.Description)
	writeJSON(w, http.StatusOK, spec.project(s.m.issueJSON(i)))
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, spec.project(s.m.issueJSON(i)))
}

// updateIssue handles the two writes the driver makes: description, and the
// State custom field.
func (s *Server) updateIssue(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	var body struct {
		Description  *string `json:"description"`
		CustomFields []struct {
			Name  string `json:"name"`
			Type  string `json:"$type"`
			Value struct {
				Name string `json:"name"`
			} `json:"value"`
		} `json:"customFields"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Description != nil {
		i.description = *body.Description
		i.updated = s.m.now()
	}
	for _, cf := range body.CustomFields {
		v := s.m.stateByName(cf.Value.Name)
		if cf.Name != s.m.stateField || cf.Type != "StateIssueCustomField" || v == nil {
			writeError(w, http.StatusBadRequest, "Bad Request", "unsupported custom field update")
			return
		}
		ts := s.m.now()
		i.updated = ts
		if v.resolved {
			if i.resolved == nil {
				i.resolved = &ts
			}
		} else {
			i.resolved = nil
		}
	}
	writeJSON(w, http.StatusOK, spec.project(s.m.issueJSON(i)))
}

func (s *Server) addIssueTag(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	t := s.m.tagByID(body.ID)
	if t == nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "unknown tag")
		return
	}
	if i.hasTag(t.id) {
		writeError(w, http.StatusConflict, "Conflict", "tag already on the issue")
		return
	}
	s.m.addTag(i, t, BotLogin)
	writeJSON(w, http.StatusOK, spec.project(s.m.tagsJSON([]string{t.id})[0]))
}

func (s *Server) removeIssueTag(w http.ResponseWriter, r *http.Request) {
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	t := s.m.tagByID(r.PathValue("tag"))
	if t == nil || !i.hasTag(t.id) {
		notFound(w, r.PathValue("tag"))
		return
	}
	s.m.removeTag(i, t, BotLogin)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listActivities(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("categories") != "TagsCategory" {
		writeError(w, http.StatusBadRequest, "Bad Request", "categories must be TagsCategory")
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	// Newest first, so a driver that does not sort is caught.
	var items []map[string]any
	for k := len(i.activities) - 1; k >= 0; k-- {
		a := i.activities[k]
		items = append(items, map[string]any{
			"$type": "TagsActivityItem", "id": a.id, "timestamp": a.timestamp, "author": userJSON(a.author),
			"added": s.m.tagsJSON(a.added), "removed": s.m.tagsJSON(a.removed),
		})
	}
	writePage(w, r, spec, items)
}

func commentJSON(c *comment) map[string]any {
	return map[string]any{"$type": "IssueComment", "id": c.id, "text": c.text, "created": c.created, "deleted": c.deleted, "author": userJSON(c.author)}
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	// Newest first, so a driver that does not sort is caught.
	var items []map[string]any
	for k := len(i.comments) - 1; k >= 0; k-- {
		items = append(items, commentJSON(i.comments[k]))
	}
	writePage(w, r, spec, items)
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	spec, ok := fields(w, r)
	if !ok {
		return
	}
	i := s.m.lookup(r.PathValue("id"))
	if i == nil {
		notFound(w, r.PathValue("id"))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "text is required")
		return
	}
	ts := s.m.now()
	c := &comment{id: s.m.nextID(4), text: body.Text, created: ts, author: BotLogin}
	i.comments = append(i.comments, c)
	i.updated = ts
	writeJSON(w, http.StatusOK, spec.project(commentJSON(c)))
}
