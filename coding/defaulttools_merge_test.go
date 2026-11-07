package coding

import (
	"reflect"
	"testing"
)

// Reference: pi v1.5.0 core/settings-manager.ts mergeDefaultTools (:126) checks
// `if (overrides === undefined) return base;` first, so a layer that does not
// set defaultTools keeps the inherited list. Pier returned nil (treating the
// result as "unset") whenever the override was absent, which dropped the global
// defaultTools whenever a project settings file set anything else.
func TestMergeDefaultToolsKeepsTheBaseWhenTheOverrideIsUnset(t *testing.T) {
	cases := []struct {
		name      string
		base      []string
		overrides []string
		want      []string
	}{
		{"base kept when overrides unset", []string{"read", "bash"}, nil, []string{"read", "bash"}},
		{"empty base kept as empty", []string{}, nil, []string{}},
		{"nothing on either side", nil, nil, nil},
		{"modifier override appends", []string{"read"}, []string{"+grep"}, []string{"read", "+grep"}},
		{"plain override replaces", []string{"read"}, []string{"write"}, []string{"write"}},
		{"nil base with modifiers", nil, []string{"+grep"}, []string{"+grep"}},
	}
	for _, testCase := range cases {
		got := mergeDefaultTools(testCase.base, testCase.overrides)
		if len(got) == 0 && len(testCase.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("%s: mergeDefaultTools(%v, %v) = %v, want %v",
				testCase.name, testCase.base, testCase.overrides, got, testCase.want)
		}
	}
}
