package interactive

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestKeybindingsAgainstUpstreamGolden replays the pinned keybindings.ts table:
// every id, its default keys and its description, on the linux non-WSL platform
// the fixture was generated for. The Windows and WSL variants are computed at
// module load upstream, so they stay covered by the focused cases in
// TestAppKeybindings and by reading keybindings.ts.
func TestKeybindingsAgainstUpstreamGolden(t *testing.T) {
	file, err := os.Open("testdata/keybindings_golden.txt")
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer file.Close()
	definitions, order := KeybindingsFor("linux", func(string) string { return "" })

	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		label, encoded, ok := strings.Cut(line, " => ")
		if !ok {
			t.Fatalf("bad golden line %q", line)
		}
		key := strings.TrimPrefix(label, "keybind ")
		var want struct {
			Keys        []string `json:"keys"`
			Description string   `json:"description"`
		}
		if err := json.Unmarshal([]byte(encoded), &want); err != nil {
			t.Fatalf("%s: golden json: %v", key, err)
		}
		got, ok := definitions[key]
		if !ok {
			t.Errorf("%s: missing from the port's table", key)
			continue
		}
		keys := got.DefaultKeys
		if keys == nil {
			keys = []string{}
		}
		if !reflect.DeepEqual(keys, want.Keys) {
			t.Errorf("%s: keys = %v, want %v", key, keys, want.Keys)
		}
		if got.Description != want.Description {
			t.Errorf("%s: description = %q, want %q", key, got.Description, want.Description)
		}
		seen[key] = true
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	for key := range definitions {
		if !seen[key] {
			t.Errorf("%s: not in the reference table", key)
		}
	}
	for _, key := range order {
		if _, ok := definitions[key]; !ok {
			t.Errorf("order names %q, which the table lacks", key)
		}
	}
	if count != len(order) {
		t.Errorf("golden has %d keys, order has %d", count, len(order))
	}
}

// Reference: pi v1.5.2 (b4afdddf) coding-agent/src/core/keybindings.ts
// `app.clipboard.pasteImage` is `windowsKeybindings ? ["alt+v", "ctrl+v"] :
// "ctrl+v"`, so Windows keeps both keys. The fixture only covers the linux
// table, which is why this case is separate.
func TestWindowsPasteImageKeepsBothKeys(t *testing.T) {
	windows, _ := KeybindingsFor("windows", func(string) string { return "" })
	if got := windows["app.clipboard.pasteImage"].DefaultKeys; !reflect.DeepEqual(got, []string{"alt+v", "ctrl+v"}) {
		t.Fatalf("windows pasteImage = %v, want [alt+v ctrl+v]", got)
	}
	wsl, _ := KeybindingsFor("linux", func(key string) string {
		if key == "WSL_INTEROP" {
			return "/run/WSL/1_interop"
		}
		return ""
	})
	if got := wsl["app.clipboard.pasteImage"].DefaultKeys; !reflect.DeepEqual(got, []string{"alt+v", "ctrl+v"}) {
		t.Fatalf("WSL pasteImage = %v, want [alt+v ctrl+v]", got)
	}
	linux, _ := KeybindingsFor("linux", func(string) string { return "" })
	if got := linux["app.clipboard.pasteImage"].DefaultKeys; !reflect.DeepEqual(got, []string{"ctrl+v"}) {
		t.Fatalf("linux pasteImage = %v, want [ctrl+v]", got)
	}
}
