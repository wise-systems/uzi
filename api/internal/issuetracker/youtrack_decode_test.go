package issuetracker

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// allocBound is the most a decode test lets the process allocate while it
// serves and decodes one oversized response. The response itself is read into a
// buffer of at most 8 MiB (grown by append, so several times that is allocated
// in total); the unbounded decode this guards against allocated about 1 GiB.
const allocBound = 64 << 20

// totalAlloc reports the bytes allocated while f runs.
func totalAlloc(f func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// emptyObjects renders a JSON array of n empty objects.
func emptyObjects(n int) string {
	return "[" + strings.Repeat("{},", n-1) + "{}]"
}

func TestListPageOverPageSizeIsRefused(t *testing.T) {
	project := `{"id":"0-1","shortName":"UZI","name":"Uzi"}`
	page := func(n int) http.HandlerFunc {
		body := "[" + strings.TrimSuffix(strings.Repeat(project+",", n), ",") + "]"
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("$skip") != "0" {
				writeRaw(w, "[]")
				return
			}
			writeRaw(w, body)
		}
	}
	y, _ := loggedDriver(t, page(pageSize))
	if got, err := y.ListProjects(context.Background()); err != nil || len(got) != pageSize {
		t.Fatalf("a page of exactly pageSize items: %d items, %v", len(got), err)
	}
	y, _ = loggedDriver(t, page(pageSize+1))
	_, err := y.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "item ceiling") {
		t.Fatalf("a page of pageSize+1 items: %v, want the item ceiling", err)
	}
}

func TestOversizedPageOfEmptyObjectsAllocatesLittle(t *testing.T) {
	n := int(listPageBytes) / 3
	body := []byte(emptyObjects(n))
	if int64(len(body)) > listPageBytes {
		t.Fatalf("fixture is %d bytes, over the page cap", len(body))
	}
	y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	var err error
	got := totalAlloc(func() { _, err = y.ListProjects(context.Background()) })
	if err == nil || !strings.Contains(err.Error(), "item ceiling") {
		t.Fatalf("err = %v, want the item ceiling", err)
	}
	if got > allocBound {
		t.Errorf("allocated %d MiB for a %d MiB page, want under %d MiB", got>>20, len(body)>>20, allocBound>>20)
	}
}

func TestTagBombAllocatesLittle(t *testing.T) {
	prefix := rawIssue("2-1", "UZI-1", 1, "0-1", "")
	prefix = strings.TrimSuffix(prefix, `"tags":[]}`) + `"tags":`
	n := int(objectBytes-int64(len(prefix))-8) / 3
	body := []byte(prefix + emptyObjects(n) + "}")
	if int64(len(body)) > objectBytes {
		t.Fatalf("fixture is %d bytes, over the object cap", len(body))
	}
	y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	var err error
	got := totalAlloc(func() { _, err = y.GetIssue(context.Background(), 1, 1) })
	if err == nil {
		t.Fatal("GetIssue accepted a tag bomb")
	}
	if got > allocBound {
		t.Errorf("allocated %d MiB for a %d MiB issue, want under %d MiB", got>>20, len(body)>>20, allocBound>>20)
	}
}

func TestListDecodeShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		wantErr bool
	}{
		"null is an empty list": {`null`, false},
		"object":                {`{}`, true},
		"trailing data":         {`[] []`, true},
		"truncated":             {`[{"id":"0-1"`, true},
	} {
		t.Run(name, func(t *testing.T) {
			y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, tc.body) })
			got, err := y.ListProjects(context.Background())
			if (err != nil) != tc.wantErr || (err == nil && len(got) != 0) {
				t.Errorf("got %v, %v; wantErr %v", got, err, tc.wantErr)
			}
		})
	}
}

func tagsJSON(n int) string {
	return strings.TrimSuffix(strings.Repeat(`{"id":"5-1","name":"a"},`, n), ",")
}

func TestNestedTagCeilingBoundary(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		n       int
		wantErr bool
	}{{maxNestedItems, false}, {maxNestedItems + 1, true}} {
		issue := rawIssue("2-1", "UZI-1", 1, "0-1", tagsJSON(tc.n))
		y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, issue) })
		got, err := y.GetIssue(ctx, 1, 1)
		if (err != nil) != tc.wantErr || (err == nil && len(got.Labels) != tc.n) {
			t.Errorf("%d tags: %d labels, %v; wantErr %v", tc.n, len(got.Labels), err, tc.wantErr)
		}
	}
}

func TestActivityNestedCeilings(t *testing.T) {
	ctx := context.Background()
	for name, act := range map[string]string{
		"added":   `[{"id":"3-1","timestamp":1,"added":[` + tagsJSON(maxNestedItems+1) + `]}]`,
		"removed": `[{"id":"3-1","timestamp":1,"removed":[` + tagsJSON(maxNestedItems+1) + `]}]`,
	} {
		t.Run(name, func(t *testing.T) {
			y, _ := loggedDriver(t, activityHandler(act))
			if got, err := y.ListIssueLabelEvents(ctx, 1, 1); err == nil {
				t.Errorf("%d events accepted for more than %d nested entries", len(got), maxNestedItems)
			}
		})
	}
	t.Run("removed only over the budget", func(t *testing.T) {
		lowerMaxItems(t, 5)
		act := `{"id":"3-1","timestamp":1,"removed":[{"id":"5-1","name":"a"},{"id":"5-2","name":"b"},{"id":"5-3","name":"c"}]}`
		y, _ := loggedDriver(t, activityHandler("["+act+","+act+"]"))
		if _, err := y.ListIssueLabelEvents(ctx, 1, 1); err == nil {
			t.Error("removed tags did not count toward the item ceiling")
		}
	})
}

func TestTopLevelItemsAndNestedTagsShareOneBudget(t *testing.T) {
	lowerMaxItems(t, 10)
	one := rawIssue("2-1", "UZI-1", 1, "0-1", `{"id":"5-1","name":"a"}`)
	y, _ := loggedDriver(t, rawListHandler(one, one, one, one, one, one))
	// Six items alone fit under 10, and so do six tags alone; together they do not.
	if _, err := y.ListIssues(context.Background(), 1, forge.ListIssuesOptions{}); err == nil {
		t.Error("6 items + 6 tags = 12 accepted under a ceiling of 10")
	}
	y, _ = loggedDriver(t, rawListHandler(one, one, one, one))
	if _, err := y.ListIssues(context.Background(), 1, forge.ListIssuesOptions{}); err != nil {
		t.Errorf("4 items + 4 tags = 8 refused under a ceiling of 10: %v", err)
	}
}
