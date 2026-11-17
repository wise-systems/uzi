package issuetracker

import "testing"

func TestClosableState(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    State
		want bool
	}{
		{"Fixed", State{Name: "Fixed", Resolved: true}, true},
		{"Done", State{Name: "Done", Resolved: true}, true},
		{"normalised", State{Name: "  FIXED\t", Resolved: true}, true},
		{"separators collapse", State{Name: "Won_t--fix", Resolved: true}, false},
		{"Won't fix", State{Name: "Won't fix", Resolved: true}, false},
		{"Duplicate", State{Name: "Duplicate", Resolved: true}, false},
		{"Obsolete", State{Name: "Obsolete", Resolved: true}, false},
		{"Can't Reproduce", State{Name: "Can't Reproduce", Resolved: true}, false},
		{"archived Fixed", State{Name: "Fixed", Resolved: true, Archived: true}, false},
		{"unresolved Done", State{Name: "Done", Resolved: false}, false},
		{"non-ASCII name, localized Fixed", State{Name: "Готово", LocalizedName: "Fixed", Resolved: true}, true},
		{"Won't fix localized as Fixed", State{Name: "Won't fix", LocalizedName: "Fixed", Resolved: true}, false},
		{"ASCII name, localized Fixed", State{Name: "Erledigt", LocalizedName: "Fixed", Resolved: true}, false},
		{"Erledigt", State{Name: "Erledigt", Resolved: true}, false},
		{"empty", State{Resolved: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClosableState(tc.s); got != tc.want {
				t.Errorf("ClosableState(%+v) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestOpenableState(t *testing.T) {
	for name, tc := range map[string]struct {
		s    State
		want bool
	}{
		"unresolved":          {State{Name: "In Progress"}, true},
		"resolved":            {State{Name: "Fixed", Resolved: true}, false},
		"archived unresolved": {State{Name: "Old", Archived: true}, false},
	} {
		if got := OpenableState(tc.s); got != tc.want {
			t.Errorf("%s: OpenableState = %v, want %v", name, got, tc.want)
		}
	}
}
