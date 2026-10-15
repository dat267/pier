package goal

import (
	"bytes"
	"encoding/json"
)

// jsonQuote encodes a string as a JSON string without HTML escaping, matching
// JavaScript's JSON.stringify for the values the machine echoes back.
func jsonQuote(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return `""`
	}
	return string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
}
