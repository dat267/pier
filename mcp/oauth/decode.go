package oauth

import (
	"encoding/json"

	"github.com/dat267/pier/mcp/protocol"
)

func unmarshalJSON(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

// marshalJSON matches the reference's JSON.stringify bodies, without HTML escaping.
func marshalJSON(value any) ([]byte, error) {
	return protocol.MarshalJSON(value)
}
