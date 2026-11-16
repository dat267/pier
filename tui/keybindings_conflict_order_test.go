package tui

import "testing"

// D486: the reference reports keybinding conflicts in the order the user config
// declared them; a Go map has no insertion order, so the port sorts both the
// claimed key and the claimant names. The order is a diagnostic detail with no
// consumer that depends on it.
func TestKeybindingConflictsAreSorted(t *testing.T) {
	definitions := map[Keybinding]KeybindingDefinition{
		"zzz.second": {DefaultKeys: []string{"ctrl+y"}},
		"aaa.first":  {DefaultKeys: []string{"ctrl+y"}},
		"mmm.third":  {DefaultKeys: []string{"ctrl+z"}},
	}
	manager := NewKeybindingsManager(definitions, map[string][]string{
		"zzz.second": {"ctrl+x"},
		"aaa.first":  {"ctrl+x"},
		"mmm.third":  {"ctrl+x"},
	})
	conflicts := manager.GetConflicts()
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one", conflicts)
	}
	names := conflicts[0].Keybindings
	want := []string{"aaa.first", "mmm.third", "zzz.second"}
	if len(names) != len(want) {
		t.Fatalf("claimants = %v, want %v", names, want)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("claimants = %v, want the sorted %v", names, want)
		}
	}
}
