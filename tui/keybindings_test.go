package tui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestFullscreenHomeEndBindingsMatchUpstream pins v1.1.0 key ownership: Home/End
// belong to the editor, while Ctrl+Home/Ctrl+End move the fullscreen transcript.
func TestFullscreenHomeEndBindingsMatchUpstream(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, nil)
	cases := []struct {
		name string
		want []string
	}{
		{"tui.editor.cursorLineStart", []string{"home", "ctrl+a"}},
		{"tui.editor.cursorLineEnd", []string{"end", "ctrl+e"}},
		{"tui.altScreen.top", []string{"ctrl+home"}},
		{"tui.altScreen.bottom", []string{"ctrl+end"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := manager.GetKeys(Keybinding(tc.name)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s bindings = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// pi v1.5.2 (b4afdddf), packages/tui/src/keybindings.ts getResolvedBindings
// returns `keys.length === 1 ? keys[0] : [...keys]`, so a single-key binding is a
// bare string and a multi-key or unset binding is an array.
func TestResolvedBindingsShape(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, nil)
	resolved := manager.GetResolvedBindings()

	single, ok := resolved["tui.editor.cursorUp"].(string)
	if !ok || single != "up" {
		t.Fatalf("cursorUp = %#v, want the bare string \"up\"", resolved["tui.editor.cursorUp"])
	}
	multiple, ok := resolved["tui.editor.cursorLeft"].([]string)
	if !ok || len(multiple) != 2 || multiple[0] != "left" || multiple[1] != "ctrl+b" {
		t.Fatalf("cursorLeft = %#v, want the two keys", resolved["tui.editor.cursorLeft"])
	}
	unset, ok := resolved["tui.editor.historyPrevious"].([]string)
	if !ok || len(unset) != 0 {
		t.Fatalf("historyPrevious = %#v, want an empty array", resolved["tui.editor.historyPrevious"])
	}
	if len(resolved) != len(TUIKeybindings) {
		t.Fatalf("resolved %d bindings, want %d", len(resolved), len(TUIKeybindings))
	}
}

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

// pi v1.4.0 packages/tui/src/keys.ts parseKey, executed over legacy, Kitty,
// function-key, mouse, modifier and astral sequences in both protocol modes.
// Regenerate:
// node tui/testdata/parse-key/generate.mjs ./pi > tui/testdata/parse-key/golden.json
func TestParseKeyMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/parse-key/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string  `json:"name"`
		Sequence string  `json:"sequence"`
		Kitty    bool    `json:"kitty"`
		Expected *string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty parse-key corpus")
	}
	previous := IsKittyProtocolActive()
	t.Cleanup(func() { SetKittyProtocolActive(previous) })
	for _, test := range cases {
		SetKittyProtocolActive(test.Kitty)
		got, ok := ParseKey(test.Sequence)
		want := ""
		if test.Expected != nil {
			want = *test.Expected
		}
		if (test.Expected == nil) == ok || got != want {
			name := test.Name
			t.Errorf("%s: ParseKey = (%q, %v), want %q present=%v", name, got, ok, want, test.Expected != nil)
		}
	}
	SetKittyProtocolActive(previous)
}

// The shape is JSON-observable: the reference's getResolvedBindings returns
// `[...keys]` for an unset binding, a real empty array, so an RPC or config
// consumer reads `[]`. A Go nil slice marshals as `null`, and the test above
// passes either way because len(nil) == 0.
func TestResolvedBindingsUnsetIsAnEmptyArrayNotNil(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, nil)
	unset, ok := manager.GetResolvedBindings()["tui.editor.historyPrevious"].([]string)
	if !ok {
		t.Fatal("historyPrevious is not a []string")
	}
	if unset == nil {
		t.Fatal("historyPrevious = nil; want a non-nil empty array like [...keys]")
	}
	encoded, err := json.Marshal(manager.GetResolvedBindings())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"tui.editor.historyPrevious":null`) {
		t.Errorf("resolved bindings encode an unset binding as null: %s", encoded)
	}
	// A keybinding with no keys in a caller-supplied definition set too.
	custom := NewKeybindingsManager(map[Keybinding]KeybindingDefinition{
		"app.none": {DefaultKeys: []string{}},
	}, nil)
	if got := custom.GetResolvedBindings()["app.none"]; got == nil {
		t.Error("app.none = nil; want an empty array")
	}
}

// getKeys is `[...(keysById.get(id) ?? [])]`, so an unset binding answers a real
// empty array as well.
func TestGetKeysUnsetIsAnEmptyArrayNotNil(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, nil)
	if keys := manager.GetKeys("tui.editor.historyPrevious"); keys == nil {
		t.Fatal("GetKeys(unset) = nil; want a non-nil empty array")
	}
	if keys := manager.GetKeys("tui.editor.cursorUp"); len(keys) != 1 || keys[0] != "up" {
		t.Fatalf("GetKeys(cursorUp) = %#v", keys)
	}
}

// A user config that binds a keybinding to an explicit empty list keeps that
// empty list in getUserBindings, as `{...this.userBindings}` does.
func TestGetUserBindingsKeepsAnExplicitEmptyList(t *testing.T) {
	manager := NewKeybindingsManager(TUIKeybindings, map[string][]string{"tui.editor.historyPrevious": {}})
	got := manager.GetUserBindings()["tui.editor.historyPrevious"]
	if got == nil {
		t.Fatal("an explicit empty list came back nil; want []")
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}
