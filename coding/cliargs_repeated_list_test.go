package coding

import "testing"

// pi v1.4.0 cli/args.ts stores listModels as a string-or-true value: each
// occurrence replaces the preceding one. A final bare flag clears filtering.
func TestParseArgsRepeatedListModelsUsesLastOccurrence(t *testing.T) {
	for _, item := range []struct {
		args []string
		all  bool
		want string
	}{
		{[]string{"--list-models", "sonnet", "--list-models"}, true, ""},
		{[]string{"--list-models", "--list-models", "sonnet"}, false, "sonnet"},
	} {
		got := ParseArgs(item.args)
		if got.ListModelsAll != item.all || item.all && got.ListModels != nil || !item.all && (got.ListModels == nil || *got.ListModels != item.want) {
			t.Errorf("%q: listModels=%v all=%v, want pattern=%q all=%v", item.args, got.ListModels, got.ListModelsAll, item.want, item.all)
		}
	}
}
