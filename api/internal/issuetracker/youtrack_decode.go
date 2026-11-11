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

// ytTags is a nested tag array. Its UnmarshalJSON streams the elements and
// refuses once more than maxNestedItems have been read, so one object cannot
// make the decoder allocate a slice as large as the response.
type ytTags []ytTag

// UnmarshalJSON implements json.Unmarshaler.
func (t *ytTags) UnmarshalJSON(b []byte) error {
	items, err := decodeArray[ytTag](b, maxNestedItems, errNestedCeiling)
	if err != nil {
		return err
	}
	*t = items
	return nil
}

// decodeArray decodes a JSON array element by element and returns refuse as
// soon as it meets element number limit+1, before that element is decoded, so at
// most limit elements are ever allocated. A JSON null is an empty array.
func decodeArray[T any](b []byte, limit int, refuse error) ([]T, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, endOfInput(dec)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, errNotArray
	}
	var items []T
	for dec.More() {
		if len(items) >= limit {
			return nil, refuse
		}
		var it T
		if err := dec.Decode(&it); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return items, endOfInput(dec)
}

// endOfInput reports errTrailingData unless dec has nothing left.
func endOfInput(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		return errTrailingData
	}
	return nil
}

// decodePage decodes one list page. The driver asked for pageSize items, so a
// longer array is a protocol violation and is refused while decoding.
func decodePage[T any](body []byte) ([]T, error) {
	return decodeArray[T](body, pageSize, errItemCeiling)
}
