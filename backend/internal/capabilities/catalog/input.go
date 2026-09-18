package catalog

import (
	"bytes"
	"encoding/json"
	"io"
)

// DecodeObject keeps versioned contracts closed to unknown fields and rejects
// null/arrays, which otherwise silently turn optional fields into broad reads.
func DecodeObject[T any](raw json.RawMessage) (T, error) {
	var value T
	data := bytes.TrimSpace(raw)
	if len(data) == 0 || data[0] != '{' {
		return value, ErrInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, ErrInput
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, ErrInput
	}
	return value, nil
}
