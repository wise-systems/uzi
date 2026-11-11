package issuetracker

import (
	"encoding/json"
	"errors"
	"io"
)

// maxNestedItems caps the tags of one issue or one activity.
const maxNestedItems = 1000

// errNestedCeiling is returned when one object carries more than maxNestedItems
// nested entries.
var errNestedCeiling = errors.New("issuetracker: an item carries more nested entries than the ceiling allows")

// Structural budgets, applied by a byte scan before any response is handed to
// json.Unmarshal. A value is counted as one comma or one opening bracket
// outside a string, an upper bound on the number of JSON values. A tag with an
// id and a name costs 3 (its brace, the comma between its members and the comma
// after it), and an activity carries up to maxNestedItems tags in each of
// "added" and "removed", so one item is allowed 6*maxNestedItems+64 and a
// single object (an issue holds one tags array) 4*maxNestedItems+256. The
// budget exists because encoding/json keeps decoding after a type error and
// builds an error per bad value, so a response of many bad values would
// otherwise allocate in proportion to its bytes, not to what it legitimately
// holds.
const (
	itemValueBudget   = 6*maxNestedItems + 64
	objectValueBudget = 4*maxNestedItems + 256
	pageValueBudget   = pageSize * itemValueBudget
)

// errValueCeiling is returned when a response holds more JSON values than its
// structural budget allows.
var errValueCeiling = errors.New("issuetracker: response holds more values than the budget allows")

// errNotArray is returned when a list response is not a JSON array.
var errNotArray = errors.New("issuetracker: response is not a JSON array")

// errTrailingData is returned when a list response has data after its array.
var errTrailingData = errors.New("issuetracker: response has data after the JSON array")

// errTagNotObject is returned when a tags array holds an element that is not an
// object, refused before decoding so a bad element costs no error value.
var errTagNotObject = errors.New("issuetracker: a tag is not a JSON object")

// ytTags is a nested tag array. Its UnmarshalJSON counts the elements with a
// byte scan and refuses above maxNestedItems before anything is decoded, so one
// object cannot make the decoder allocate a slice as large as the response. It
// builds no json.Decoder: encoding/json calls it once per repeated "tags" key,
// and a per-call Decoder would multiply across a response of repeated keys.
type ytTags []ytTag

// UnmarshalJSON implements json.Unmarshaler. b is already-validated JSON.
func (t *ytTags) UnmarshalJSON(b []byte) error {
	if n, _ := topLevelCount(b); n > maxNestedItems {
		return errNestedCeiling
	}
	if !onlyObjects(b) {
		return errTagNotObject
	}
	return json.Unmarshal(b, (*[]ytTag)(t))
}

// topLevelCount scans b, a JSON value, and returns the number of elements of
// the array it starts with (0 when it does not start with an array) and the
// offset just past that array (0 when it does not start with one, or never
// closes). It tracks bracket depth and string state only, so it neither
// validates nor allocates; a caller that needs validity gets it from
// json.Unmarshal afterwards.
func topLevelCount(b []byte) (count, end int) {
	i := 0
	for i < len(b) && isJSONSpace(b[i]) {
		i++
	}
	if i >= len(b) || b[i] != '[' {
		return 0, 0
	}
	depth, seen, inStr, esc := 0, false, false, false
	for ; i < len(b); i++ {
		c := b[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '[', '{':
			if depth == 1 {
				seen = true
			}
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				if seen {
					count++
				}
				return count, i + 1
			}
		case ',':
			if depth == 1 {
				count++
			}
		case '"':
			if depth == 1 {
				seen = true
			}
			inStr = true
		default:
			if depth == 1 && !isJSONSpace(c) {
				seen = true
			}
		}
	}
	return count, 0
}

// onlyObjects reports whether b, a JSON value, is null or an array whose every
// element is an object or null. It looks only at the first byte of each
// element, so it is meaningful on already-validated JSON; any other first byte
// (a number, string, array, true, false) is refused.
func onlyObjects(b []byte) bool {
	i := 0
	for i < len(b) && isJSONSpace(b[i]) {
		i++
	}
	if i >= len(b) {
		return true
	}
	if b[i] == 'n' {
		return true
	}
	if b[i] != '[' {
		return false
	}
	depth, inStr, esc, want := 0, false, false, false
	for ; i < len(b); i++ {
		c := b[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if want && !isJSONSpace(c) {
			if c != '{' && c != 'n' && c != ']' {
				return false
			}
			want = false
		}
		switch c {
		case '[', '{':
			depth++
			want = depth == 1
		case ']', '}':
			depth--
			if depth == 0 {
				return true
			}
		case ',':
			want = depth == 1
		case '"':
			inStr = true
		}
	}
	return true
}

// valueCount returns the number of commas and opening brackets outside strings
// in b, at every depth: an upper bound on the number of JSON values.
func valueCount(b []byte) int {
	n, inStr, esc := 0, false, false
	for _, c := range b {
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case ',', '[', '{':
			n++
		}
	}
	return n
}

// trimJSONSpace trims JSON's own whitespace (space, tab, CR, LF) from both ends.
func trimJSONSpace(b []byte) []byte {
	for len(b) > 0 && isJSONSpace(b[0]) {
		b = b[1:]
	}
	for len(b) > 0 && isJSONSpace(b[len(b)-1]) {
		b = b[:len(b)-1]
	}
	return b
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// decodePage decodes one list page. The driver asked for pageSize items, so a
// longer array is a protocol violation: the elements and the structural values
// are counted by byte scans before any element is decoded, so an oversized page
// allocates nothing for its items. A JSON null is an empty page. Whitespace is
// JSON's own, so a form feed is a syntax error rather than padding.
func decodePage[T any](body []byte) ([]T, error) {
	trimmed := trimJSONSpace(body)
	if len(trimmed) >= 4 && string(trimmed[:4]) == "null" {
		switch {
		case len(trimmed) == 4:
			return nil, nil
		case isJSONSpace(trimmed[4]):
			return nil, errTrailingData
		}
	}
	n, end := topLevelCount(body)
	if end == 0 {
		if len(trimmed) > 0 && trimmed[0] == '[' {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, errNotArray
	}
	if n > pageSize {
		return nil, errItemCeiling
	}
	if len(trimJSONSpace(body[end:])) > 0 {
		return nil, errTrailingData
	}
	if valueCount(body) > pageValueBudget {
		return nil, errValueCeiling
	}
	var items []T
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	return items, nil
}
