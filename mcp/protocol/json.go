package protocol

import "github.com/dat267/pier/ai"

// MarshalJSON encodes v the way JavaScript's JSON.stringify does: no HTML
// escaping of <, > and &. The MCP reference serializes every wire message with
// JSON.stringify (streamable-http.ts, stdio.ts), so escaping here would diverge
// from the reference bytes.
func MarshalJSON(v any) ([]byte, error) {
	return ai.MarshalJSON(v)
}
