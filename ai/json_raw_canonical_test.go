package ai

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Reference: pi v1.4.0 core/session-manager.ts and agent/src/proxy.ts use
// JSON.stringify on parsed values, including imported/nested raw payloads.
func TestMarshalRawJSONCanonicalizesJavaScriptValues(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"text":"\u0061\/\u4f60\u597d"}`, `{"text":"a/你好"}`},
		{`{"10":1,"2":2,"a":3}`, `{"2":2,"10":1,"a":3}`},
		{`{"x":1,"y":0,"x":2}`, `{"x":2,"y":0}`},
		{`{"outer":[{"10":1,"2":2,"a":"\u0061"}],"n":9223372036854775807}`, `{"outer":[{"2":2,"10":1,"a":"a"}],"n":9223372036854775807}`},
	} {
		raw := json.RawMessage(tc.raw)
		got, err := MarshalJSON(raw)
		if err != nil || string(got) != tc.want {
			t.Errorf("raw=%s got=%s err=%v want=%s", tc.raw, got, err, tc.want)
		}
		// Indentation must not prevent canonicalization.
		indented, err := MarshalIndentJSON(struct {
			Data json.RawMessage `json:"data"`
		}{raw})
		if err != nil {
			t.Fatal(err)
		}
		var nested struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(indented, &nested); err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, nested.Data); err != nil {
			t.Fatal(err)
		}
		if compact.String() != tc.want {
			t.Errorf("indented=%s want data=%s", indented, tc.want)
		}
	}
}
