package issuetracker

import (
	"context"
	"net/http"
	"net/url"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// listTags returns the tags the token can see, keyed by name.
func (y *youTrack) listTags(ctx context.Context) (map[string]ytTag, []ytTag, error) {
	tags, err := ytList[ytTag](ctx, y, []string{"tags"}, url.Values{"fields": {tagFields}})
	if err != nil {
		return nil, nil, err
	}
	byName := make(map[string]ytTag, len(tags))
	for _, t := range tags {
		if !entityIDRE.MatchString(t.ID) {
			return nil, nil, y.fail("youtrack: tag with a malformed id in the response")
		}
		if _, dup := byName[t.Name]; !dup {
			byName[t.Name] = t
		}
	}
	return byName, tags, nil
}

// createTag creates a tag by name and returns it.
func (y *youTrack) createTag(ctx context.Context, name string) (ytTag, error) {
	var t ytTag
	_, _, err := y.do(ctx, ytCall{
		method: http.MethodPost,
		path:   []string{"tags"},
		query:  url.Values{"fields": {tagFields}},
		body:   map[string]string{"name": name},
		limit:  objectBytes,
		prov:   forge.ResponseSuccess,
	}, &t)
	if err != nil {
		return ytTag{}, err
	}
	if !entityIDRE.MatchString(t.ID) {
		return ytTag{}, y.fail("youtrack: tag with a malformed id in the response")
	}
	return t, nil
}

// tagIDsFor maps names to the ids of their tags, creating a tag that does not
// exist yet. Duplicate names are created once.
func (y *youTrack) tagIDsFor(ctx context.Context, names []string) (map[string]string, error) {
	byName, _, err := y.listTags(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(names))
	for _, n := range names {
		t, ok := byName[n]
		if !ok {
			if t, err = y.createTag(ctx, n); err != nil {
				return nil, err
			}
			byName[n] = t
		}
		ids[n] = t.ID
	}
	return ids, nil
}

// ListLabels implements forge.IssueTracker. Labels are YouTrack tags, global
// to the account; projectID is ignored.
func (y *youTrack) ListLabels(ctx context.Context, _ int64) ([]forge.Label, error) {
	if _, err := y.requireProject(); err != nil {
		return nil, err
	}
	_, tags, err := y.listTags(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Label, 0, len(tags))
	for _, t := range tags {
		out = append(out, forge.Label{Name: t.Name})
	}
	return out, nil
}

// EnsureLabels implements forge.IssueTracker: it creates the tags that do not
// exist by name. Label.Color is ignored, because YouTrack colours are palette
// ids.
func (y *youTrack) EnsureLabels(ctx context.Context, _ int64, labels []forge.Label) error {
	if _, err := y.requireProject(); err != nil {
		return err
	}
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}
	if err := y.checkLabelNames(names); err != nil {
		return err
	}
	_, err := y.tagIDsFor(ctx, names)
	return err
}

// UpdateIssueLabels implements forge.IssueTracker. It makes one REST call per
// tag, adds first and then removes, so it is not atomic. The commands API is
// not used, so a label name never enters a command query. A tag to add that
// does not exist is created; one to remove that is not on the issue is skipped.
func (y *youTrack) UpdateIssueLabels(ctx context.Context, _, issueIID int64, add, remove []string) error {
	if _, err := y.requireProject(); err != nil {
		return err
	}
	if err := y.checkLabelNames(add, remove); err != nil {
		return err
	}
	w, err := y.resolveIssue(ctx, issueIID)
	if err != nil {
		return err
	}
	present := make(map[string]string, len(w.Tags))
	for _, t := range w.Tags {
		present[t.Name] = t.ID
	}
	var missing []string
	for _, n := range add {
		if _, ok := present[n]; !ok {
			missing = append(missing, n)
		}
	}
	var ids map[string]string
	if len(missing) > 0 {
		if ids, err = y.tagIDsFor(ctx, missing); err != nil {
			return err
		}
	}
	for _, n := range missing {
		if _, ok := present[n]; ok {
			continue
		}
		if err := y.attachTag(ctx, w.ID, ids[n]); err != nil {
			return err
		}
		present[n] = ids[n]
	}
	for _, n := range remove {
		id, ok := present[n]
		if !ok {
			continue
		}
		if err := y.detachTag(ctx, w.ID, id); err != nil {
			return err
		}
		delete(present, n)
	}
	return nil
}

func (y *youTrack) attachTag(ctx context.Context, issueID, tagID string) error {
	_, _, err := y.do(ctx, ytCall{
		method: http.MethodPost,
		path:   []string{"issues", issueID, "tags"},
		query:  url.Values{"fields": {tagFields}},
		body:   map[string]string{"id": tagID},
		limit:  objectBytes,
		prov:   forge.ResponseSuccess,
	}, nil)
	return err
}

func (y *youTrack) detachTag(ctx context.Context, issueID, tagID string) error {
	_, _, err := y.do(ctx, ytCall{
		method: http.MethodDelete,
		path:   []string{"issues", issueID, "tags", tagID},
		limit:  objectBytes,
		prov:   forge.ResponseSuccess,
	}, nil)
	return err
}
