package youtracktest

// SeedIssue creates an issue in the main project and returns its number. Tags
// are created on demand and added without recording an activity.
func (s *Server) SeedIssue(summary, description string, tags ...string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.m.newIssue(s.m.projectByID(ProjectID), summary, description)
	for _, name := range tags {
		i.tags = append(i.tags, s.m.ensureTag(name).id)
	}
	return i.number
}

// SeedComment adds a comment by login on issue number n.
func (s *Server) SeedComment(n int64, login, text string) {
	s.seedComment(n, login, text, false)
}

// SeedDeletedComment adds a comment that the API reports as deleted.
func (s *Server) SeedDeletedComment(n int64, login, text string) {
	s.seedComment(n, login, text, true)
}

func (s *Server) seedComment(n int64, login, text string, deleted bool) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.m.byNumber(n)
	if i == nil {
		s.t.Fatalf("youtracktest: no issue %d", n)
	}
	ts := s.m.now()
	i.updated = ts
	i.comments = append(i.comments, &comment{id: s.m.nextID(4), text: text, created: ts, deleted: deleted, author: login})
}

// SeedTagActivity records that login added and removed the named tags on issue
// number n. Tags are created on demand; the issue's own tag list is left
// alone, so a test can seed history that no longer matches the present.
func (s *Server) SeedTagActivity(n int64, login string, added, removed []string) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.m.byNumber(n)
	if i == nil {
		s.t.Fatalf("youtracktest: no issue %d", n)
	}
	a := &activity{id: s.m.nextID(3), timestamp: s.m.now(), author: login}
	for _, name := range added {
		a.added = append(a.added, s.m.ensureTag(name).id)
	}
	for _, name := range removed {
		a.removed = append(a.removed, s.m.ensureTag(name).id)
	}
	i.activities = append(i.activities, a)
}

// MoveIssue moves issue number n to the project toProjectID. Like YouTrack it
// takes a new number there, and the old id-readable keeps resolving to it. It
// returns the new id-readable.
func (s *Server) MoveIssue(n int64, toProjectID string) string {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.m.byNumber(n)
	to := s.m.projectByID(toProjectID)
	if i == nil || to == nil {
		s.t.Fatalf("youtracktest: cannot move issue %d to project %q", n, toProjectID)
	}
	to.next++
	i.project, i.number = to, to.next
	i.updated = s.m.now()
	s.m.aliases[i.readable()] = i
	return i.readable()
}

// IssueTags returns the names of the tags currently on issue number n in the
// main project, for assertions on server-side state.
func (s *Server) IssueTags(n int64) []string {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.m.byNumber(n)
	if i == nil {
		s.t.Fatalf("youtracktest: no issue %d", n)
	}
	var names []string
	for _, id := range i.tags {
		names = append(names, s.m.tagByID(id).name)
	}
	return names
}
