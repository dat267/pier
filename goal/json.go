package goal

import (
	"bytes"
	"encoding/json"
)

// marshalString encodes a string as a JSON string without HTML escaping.
func marshalString(value string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))), nil
}
