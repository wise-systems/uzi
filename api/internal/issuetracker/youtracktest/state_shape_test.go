package youtracktest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

type shapeIssue struct {
	IDReadable   string           `json:"idReadable"`
	CustomFields []map[string]any `json:"customFields"`
}

func decodeShape(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
}

func TestStateFieldShapes(t *testing.T) {
	const write = `{"customFields":[{"name":"State","$type":"StateIssueCustomField","value":{"id":"%s"}}]}`
	for _, tc := range []struct {
		name      string
		shape     StateFieldShape
		wantEntry bool
		wantType  bool
		wantWrite int
	}{
		{"plain", StateFieldPlain, true, true, http.StatusOK},
		{"absent", StateFieldAbsent, false, false, http.StatusBadRequest},
		{"untyped", StateFieldUntyped, true, false, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(t)
			n := srv.SeedIssue("s", "d")
			srv.SeedStateFieldShape(n, tc.shape)
			id := "UZI-" + strconv.FormatInt(n, 10)

			check := func(label string, it shapeIssue) {
				t.Helper()
				var entry map[string]any
				for _, cf := range it.CustomFields {
					if cf["name"] == "State" {
						entry = cf
					}
				}
				if (entry != nil) != tc.wantEntry {
					t.Fatalf("%s: State entry present = %v, want %v: %v", label, entry != nil, tc.wantEntry, it.CustomFields)
				}
				if entry != nil {
					if _, has := entry["$type"]; has != tc.wantType {
						t.Errorf("%s: State entry has $type = %v, want %v", label, has, tc.wantType)
					}
				}
			}

			status, body := call(t, srv, http.MethodGet, "/api/issues/"+id+"?fields=id,customFields(name,$type,value(name))", "")
			if status != http.StatusOK {
				t.Fatalf("get: status %d: %s", status, body)
			}
			var one shapeIssue
			decodeShape(t, body, &one)
			check("get", one)

			q := url.Values{"query": {"project: {UZI}"}, "fields": {"id,idReadable,customFields(name,$type,value(name))"}, "customFields": {"State"}}
			status, body = call(t, srv, http.MethodGet, "/api/issues?"+q.Encode(), "")
			if status != http.StatusOK {
				t.Fatalf("list: status %d: %s", status, body)
			}
			var list []shapeIssue
			decodeShape(t, body, &list)
			if len(list) != 1 {
				t.Fatalf("list: %d issues, want 1: %s", len(list), body)
			}
			check("list", list[0])

			var stateID string
			srv.mu.Lock()
			for _, st := range srv.m.states {
				if !st.resolved && !st.archived {
					stateID = st.id
					break
				}
			}
			srv.mu.Unlock()
			status, body = call(t, srv, http.MethodPost, "/api/issues/"+id+"?fields=id", fmt.Sprintf(write, stateID))
			if status != tc.wantWrite {
				t.Errorf("write: status %d, want %d: %s", status, tc.wantWrite, body)
			}
			_ = srv.IssueState(n) // must not panic on any shape
		})
	}
}

func TestSeedIDReadableOverridesOnlyTheWireValue(t *testing.T) {
	for _, override := range []string{"", "UZI-LONG-1234567890123456789012345678901234567890"} {
		srv := NewServer(t)
		n := srv.SeedIssue("s", "d")
		srv.SeedIDReadable(n, override)
		status, body := call(t, srv, http.MethodGet, "/api/issues/UZI-"+strconv.FormatInt(n, 10)+"?fields=idReadable", "")
		if status != http.StatusOK {
			t.Fatalf("get by the real id-readable: status %d: %s", status, body)
		}
		var it shapeIssue
		decodeShape(t, body, &it)
		if it.IDReadable != override {
			t.Errorf("idReadable = %q, want %q", it.IDReadable, override)
		}
	}
}
