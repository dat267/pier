package tui

import (
	"reflect"
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
