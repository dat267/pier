package coding

import (
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 core/model-resolver.ts bestFuzzyMatch sorts IDs with
// descending localeCompare. Match Node's default English/root collation.
func TestFuzzyModelAliasLocaleOrdering(t *testing.T) {
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{[]string{"model-2", "model_1"}, "model-2"},
		{[]string{"model-Z", "model-z"}, "model-Z"},
		// D257: root collation is deterministic even on a Swedish host,
		// whose Node localeCompare would place ä after z.
		{[]string{"model-ä", "model-z"}, "model-z"},
	} {
		models := []*ai.Model{{ID: tc.ids[0], Provider: "p"}, {ID: tc.ids[1], Provider: "p"}}
		got := ResolveModelReference(ModelReferenceRequest{Text: "model", Fuzzy: true, ThinkingSuffix: "none"}, models, func(string) bool { return true })
		if got.Model == nil || got.Model.ID != tc.want {
			t.Fatalf("ids=%v: %+v; want %s", tc.ids, got, tc.want)
		}
	}
}
