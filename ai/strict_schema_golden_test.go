package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Replays the strict-schema goldens generated from the pinned reference's
// makeStrictJsonSchema (v1.5.2 api/constrained-sampling.ts): the strict JSON
// must match byte for byte, including key order, and the rejection messages
// must match too.
func TestStrictSchemaMatchesThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/strict-schema/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Schema     json.RawMessage `json:"schema"`
		StrictJSON string          `json:"strictJson"`
		Error      string          `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty strict-schema golden")
	}
	for index, testCase := range cases {
		got, err := MakeStrictJSONSchema(testCase.Schema)
		if testCase.Error != "" {
			if err == nil {
				t.Fatalf("case %d (%s): want error %q, got %s", index, testCase.Schema, testCase.Error, got)
			}
			if err.Error() != testCase.Error {
				t.Fatalf("case %d (%s): error = %q, want %q", index, testCase.Schema, err.Error(), testCase.Error)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d (%s): %v", index, testCase.Schema, err)
		}
		if string(got) != testCase.StrictJSON {
			t.Fatalf("case %d (%s):\n got %s\nwant %s", index, testCase.Schema, got, testCase.StrictJSON)
		}
	}
}
