package issuetracker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/vtmocanu/uzi/api/internal/forge"
)

// The allocation tests below read the process-wide runtime.MemStats
// TotalAlloc, so they must not run in parallel with each other or with any
// other test: do not add t.Parallel to them.

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

// TestDuplicateTagsKeysAllocateLittle pins that encoding/json calling
// ytTags.UnmarshalJSON once per repeated "tags" key costs no per-call Decoder:
// the unfixed decoder allocated about 380 MiB for the page fixture. The fixed
// decoder allocates about 30 MiB without -race and about 100 MiB with it (the
// race build makes sync.Pool drop entries, so encoding/json's own pooled state
// is reallocated), so the bound sits between the two.
func TestDuplicateTagsKeysAllocateLittle(t *testing.T) {
	const bound = 192 << 20
	// The fixed single-issue decode allocates about 4 MiB (25 MiB with -race);
	// the unfixed one allocated about 99 MiB (120 MiB with -race).
	const issueBound = 48 << 20
	t.Run("page", func(t *testing.T) {
		issue := strings.TrimSuffix(rawIssue("2-1", "UZI-1", 1, "0-1", ""), `"tags":[]}`)
		dup := `"tags":[{}],`
		body := "[" + issue + strings.Repeat(dup, int(listPageBytes-int64(len(issue))-64)/len(dup)) + `"tags":[]}]`
		if int64(len(body)) > listPageBytes {
			t.Fatalf("fixture is %d bytes, over the page cap", len(body))
		}
		y, _ := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("$skip") != "0" {
				writeRaw(w, "[]")
				return
			}
			_, _ = w.Write([]byte(body))
		})
		var err error
		got := totalAlloc(func() { _, err = y.ListIssues(context.Background(), 1, forge.ListIssuesOptions{}) })
		if err != nil {
			t.Logf("ListIssues: %v", err)
		}
		if got > bound {
			t.Errorf("allocated %d MiB for a %d MiB page, want under %d MiB", got>>20, len(body)>>20, bound>>20)
		}
	})
	t.Run("issue", func(t *testing.T) {
		issue := strings.TrimSuffix(rawIssue("2-1", "UZI-1", 1, "0-1", ""), `"tags":[]}`)
		dup := `"tags":[{}],`
		body := []byte(issue + strings.Repeat(dup, int(objectBytes-int64(len(issue))-64)/len(dup)) + `"tags":[]}`)
		if int64(len(body)) > objectBytes {
			t.Fatalf("fixture is %d bytes, over the object cap", len(body))
		}
		y, _ := loggedDriver(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
		got := totalAlloc(func() { _, _ = y.GetIssue(context.Background(), 1, 1) })
		if got > issueBound {
			t.Errorf("allocated %d MiB for a %d MiB issue, want under %d MiB", got>>20, len(body)>>20, issueBound>>20)
		}
	})
}

func TestListDecodeShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		want    error
		wantLen int
	}{
		"null is an empty list":       {`null`, nil, 0},
		"null padded with JSON space": {" \t\r\nnull\n ", nil, 0},
		"form feed is not JSON space": {"\fnull", errNotArray, 0},
		"data after null":             {`null x`, errTrailingData, 0},
		"object":                      {`{}`, errNotArray, 0},
		"trailing data":               {`[] []`, errTrailingData, 0},
		"truncated":                   {`[{"id":"0-1"`, io.ErrUnexpectedEOF, 0},
		"over the item ceiling":       {emptyObjects(pageSize + 1), errItemCeiling, 0},
		"exactly the item ceiling":    {emptyObjects(pageSize), nil, pageSize},
		"repeated bad values":         {`[{` + strings.Repeat(`"a":"",`, pageValueBudget) + `"a":""}]`, errValueCeiling, 0},
		"form feed after the array":   {"[]\f", errTrailingData, 0},
		"JSON space after the array":  {"[] \n", nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodePage[map[string]any]([]byte(tc.body))
			if !errors.Is(err, tc.want) || len(got) != tc.wantLen {
				t.Errorf("got %d items, %v; want %d items, %v", len(got), err, tc.wantLen, tc.want)
			}
		})
	}
}

// TestRepeatedBadValuesAllocateLittle pins that a response of many values that
// fail to decode is refused by the structural budget before encoding/json
// builds an error per value (the unfixed decoder allocated about 510 MiB for the
// repeated-tags page; the fixed one allocates the response buffer only).
func TestRepeatedBadValuesAllocateLittle(t *testing.T) {
	issue := strings.TrimSuffix(rawIssue("2-1", "UZI-1", 1, "0-1", ""), `"tags":[]}`)
	fill := func(dup string, limit int64, head string) string {
		return head + strings.Repeat(dup, int(limit-int64(len(head))-64)/len(dup))
	}
	badTags := `"tags":[` + strings.Repeat("1,", 999) + `1],`
	for name, tc := range map[string]struct {
		dup  string
		page bool
	}{
		"page of bad tags":     {badTags, true},
		"issue of bad tags":    {badTags, false},
		"page of bad created":  {`"created":"",`, true},
		"issue of bad created": {`"created":"",`, false},
	} {
		t.Run(name, func(t *testing.T) {
			var body string
			if tc.page {
				body = "[" + fill(tc.dup, listPageBytes-2, issue) + `"tags":[]}]`
			} else {
				body = fill(tc.dup, objectBytes, issue) + `"tags":[]}`
			}
			y, _ := loggedDriver(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("$skip") != "0" && tc.page {
					writeRaw(w, "[]")
					return
				}
				_, _ = w.Write([]byte(body))
			})
			var err error
			got := totalAlloc(func() {
				if tc.page {
					_, err = y.ListIssues(context.Background(), 1, forge.ListIssuesOptions{})
				} else {
					_, err = y.GetIssue(context.Background(), 1, 1)
				}
			})
			if err == nil {
				t.Error("a response of repeated bad values was accepted")
			}
			bound := uint64(repeatedIssueBound)
			if tc.page {
				bound = repeatedPageBound
			}
			if got > bound {
				t.Errorf("allocated %d MiB for a %d MiB response, want under %d MiB", got>>20, len(body)>>20, bound>>20)
			}
		})
	}
}

// Bounds for TestRepeatedBadValuesAllocateLittle, in bytes allocated. Measured
// without and with -race: fixed page 24 and 41 MiB (the response buffer), fixed
// issue 6 and 11 MiB; unbudgeted page 118 to 615 MiB, unbudgeted issue of bad
// tags 134 and 155 MiB (the unbudgeted issue of bad created values allocates
// 26 and 33 MiB, so that case is a plain regression check, not a discriminator).
const (
	repeatedPageBound  = 64 << 20
	repeatedIssueBound = 20 << 20
)

func TestTagElementsMustBeObjects(t *testing.T) {
	for name, tc := range map[string]struct {
		in string
		ok bool
	}{
		"empty":         {`[]`, true},
		"objects":       {`[{"id":"a"} , {}]`, true},
		"null":          {`null`, true},
		"null element":  {`[null]`, true},
		"number":        {`[1]`, false},
		"later number":  {`[{},1]`, false},
		"string":        {`["{"]`, false},
		"nested array":  {`[[{}]]`, false},
		"brace in text": {`[{"a":"1,2"},{"b":[1,2]}]`, true},
	} {
		var tags ytTags
		err := tags.UnmarshalJSON([]byte(tc.in))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", name, err, tc.ok)
		}
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

func TestTopLevelCountIgnoresBracketsInStrings(t *testing.T) {
	for name, tc := range map[string]struct {
		in    string
		count int
	}{
		"empty":             {`[]`, 0},
		"spaced empty":      {` [ ] `, 0},
		"scalars":           {`[1, 2,3]`, 3},
		"nested":            {`[[1,2],{"a":[3,4]},5]`, 3},
		"string with ,]":    {`["a,]","b\"],"]`, 2},
		"escaped backslash": {`["\\",1]`, 2},
		"not an array":      {`{"a":[1,2]}`, 0},
	} {
		if got, _ := topLevelCount([]byte(tc.in)); got != tc.count {
			t.Errorf("%s: count %d, want %d", name, got, tc.count)
		}
	}
}
