package issuetracker

import (
	"fmt"
	"strings"
	"unicode"
)

// closableNames are the normalised state names that mean the work is done. It
// is an allowlist on purpose: isResolved is true for "Won't fix" and
// "Duplicate" too, and a name this list does not know is refused rather than
// guessed at, so a write never closes an issue as something it was not.
var closableNames = map[string]bool{
	"fixed": true, "done": true, "resolved": true, "verified": true,
	"completed": true, "complete": true, "closed": true, "implemented": true,
	"shipped": true, "released": true, "delivered": true, "merged": true,
	"finished": true,
}

// ClosableState reports whether s may be written as the closed state: it is
// resolved, not archived, and its name is one of closableNames once
// normalised, or its localized name is when the name is not pure ASCII.
func ClosableState(s State) bool {
	if !s.Resolved || s.Archived {
		return false
	}
	if closableNames[normaliseStateName(s.Name)] {
		return true
	}
	// The localized name is consulted only for a name that is not pure ASCII (a
	// non-English name with an English localized one). For an ASCII name it
	// could otherwise relabel "Won't fix" as "Fixed".
	return !isASCII(s.Name) && closableNames[normaliseStateName(s.LocalizedName)]
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// OpenableState reports whether s may be written as the open state or as a
// board column's state: it is unresolved and not archived.
func OpenableState(s State) bool { return !s.Resolved && !s.Archived }

// normaliseStateName lower-cases name, trims it and collapses each run of
// whitespace, '-' and '_' to one space.
func normaliseStateName(name string) string {
	fields := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == '_'
	})
	return strings.Join(fields, " ")
}

// stateNameProblem reports whether a State name read from the issue tracker is
// one the driver will not pass on: blank, over maxLabelNameLen bytes, or
// carrying an invisible or control character. Any user who administers a bundle
// can name a state, and the name ends up in errors and in the board.
func stateNameProblem(name string) bool {
	if strings.TrimSpace(name) == "" || len(name) > maxLabelNameLen {
		return true
	}
	for _, c := range name {
		if InvisibleRune(c) {
			return true
		}
	}
	return false
}

// maxQuotedNameLen bounds the name quoteName puts in an error.
const maxQuotedNameLen = 80

// quoteName renders a configured state name for an error: cut to
// maxQuotedNameLen bytes and quoted with %q, so no control character survives.
func quoteName(name string) string { return fmt.Sprintf("%q", truncate(name, maxQuotedNameLen)) }
