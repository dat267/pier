package ai

import (
	"encoding/json"
	"testing"
)

// FuzzMarshalJSONScalarsMatchGeneral keeps the scalar and raw fast paths
// byte-identical to the encoder+canonicalize reference (M143).
func FuzzMarshalJSONScalarsMatchGeneral(f *testing.F) {
	for _, seed := range []string{"", "a", "\u2028", "\xff", "<&>", "01", "{}", "1 2", "null", `{"10":1,"a":2}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 256 {
			return
		}
		for _, value := range []any{string(data), json.RawMessage(data)} {
			got, gotErr := MarshalJSON(value)
			want, wantErr := marshalJSONGeneral(value)
			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("%T %q: err = %v, want %v", value, data, gotErr, wantErr)
			}
			if gotErr == nil && string(got) != string(want) {
				t.Fatalf("%T %q = %q, want %q", value, data, got, want)
			}
		}
	})
}
