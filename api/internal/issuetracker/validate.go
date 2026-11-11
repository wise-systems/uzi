package issuetracker

import (
	"errors"
	"unicode"
)

// ValidateToken rejects a token a driver would not accept: shorter than
// minTokenLen (too short to redact safely), longer than maxTokenLen, or holding
// a control character, a Unicode format character or a line or paragraph
// separator. The message never includes the token.
func ValidateToken(token string) error {
	if len(token) < minTokenLen {
		return errors.New("issuetracker: token shorter than the minimum length")
	}
	if len(token) > maxTokenLen {
		return errors.New("issuetracker: token longer than the maximum length")
	}
	for _, c := range token {
		if InvisibleRune(c) {
			return errors.New("issuetracker: token contains a control or format character")
		}
	}
	return nil
}

// InvisibleRune reports whether c is a control character, a Unicode format
// character (bidi controls, zero-width characters, BOM) or the line or
// paragraph separator.
func InvisibleRune(c rune) bool {
	return unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || c == '\u2028' || c == '\u2029'
}

// ValidProjectID reports whether id is a well-formed project entity id such as
// "0-1".
func ValidProjectID(id string) bool { return entityIDRE.MatchString(id) }

// ValidProjectRef accepts the empty ProjectRef (no project bound) and
// otherwise requires a well-formed id and short name. The message never
// includes the input.
func ValidProjectRef(ref ProjectRef) error {
	if ref == (ProjectRef{}) {
		return nil
	}
	if !shortNameRE.MatchString(ref.ShortName) {
		return errors.New("issuetracker: invalid project short name")
	}
	if !ValidProjectID(ref.ID) {
		return errors.New("issuetracker: invalid project id")
	}
	return nil
}
