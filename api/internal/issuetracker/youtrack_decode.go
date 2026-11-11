package issuetracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// maxNestedItems caps the tags of one issue or one activity.
const maxNestedItems = 1000

// errNestedCeiling is returned when one object carries more than maxNestedItems
// nested entries.
var errNestedCeiling = errors.New("issuetracker: an item carries more nested entries than the ceiling allows")

// errNotArray is returned when a list response is not a JSON array.
var errNotArray = errors.New("issuetracker: response is not a JSON array")

// errTrailingData is returned when a list response has data after its array.
var errTrailingData = errors.New("issuetracker: response has data after the JSON array")

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

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// decodePage decodes one list page. The driver asked for pageSize items, so a
// longer array is a protocol violation: the elements are counted before any is
// decoded, so an oversized page allocates nothing for its items. A JSON null is
// an empty page.
func decodePage[T any](body []byte) ([]T, error) {
	trimmed := bytes.TrimSpace(body)
	if string(trimmed) == "null" {
		return nil, nil
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
	if len(bytes.TrimSpace(body[end:])) > 0 {
		return nil, errTrailingData
	}
	var items []T
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	return items, nil
}
