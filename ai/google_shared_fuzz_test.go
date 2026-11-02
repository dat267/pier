package ai

// The Gemini const-to-enum rewrite walks untrusted tool schemas: valid JSON must
// stay valid JSON, and no shape may panic (pi v1.5.0 rewriteConstAsEnum).

import (
	"encoding/json"
	"testing"
)

func FuzzRewriteGoogleConstAsEnum(f *testing.F) {
	for _, seed := range []string{
		`{"const":"x"}`,
		`{"properties":{"const":{"const":1}}}`,
		`{"allOf":[{"const":true}]}`,
		`[]`,
		`"x"`,
		`{"enum":[],"const":3}`,
		`{"properties":{"a":{"type":"array","items":{"const":2}}}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out := rewriteGoogleConstAsEnum(json.RawMessage(raw), false)
		if json.Valid([]byte(raw)) && !json.Valid(out) {
			t.Fatalf("valid input produced invalid JSON: %q -> %q", raw, out)
		}
	})
}
