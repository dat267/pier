package coding

import "testing"

// FuzzSettingsRawOrders feeds arbitrary bytes to the settings-file readers the
// save path uses to keep JavaScript key order. A settings file is hand-editable
// and may be malformed, so these must never panic; the save path treats an
// unreadable file as a parse error and leaves it alone.
func FuzzSettingsRawOrders(f *testing.F) {
	f.Add(`{"terminal":{"showImages":true,"imageWidthCells":30}}`)
	f.Add(`{"packages":["a",{"source":"b","skills":["s"]}]}`)
	f.Add(`{"terminal":null,"retry":[]}`)
	f.Add(`{"terminal":{"a":1,"b":{"c":2}}}`)
	f.Add(`[]`)
	f.Add(`{`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, text string) {
		_ = settingsRawKeyOrder(text)
		_ = settingsRawFieldValue(text, "terminal")
		_ = settingsRawNestedKeyOrder(text, "terminal")
		_ = settingsRawArrayElementOrders(text, "packages")
		merged := map[string]any{
			"terminal": map[string]any{"showImages": true},
			"packages": []any{"a", map[string]any{"source": "b"}},
		}
		_ = nestedKeyOrder(text, merged, map[string][]string{"terminal": {"showImages"}})
		_ = arrayFieldOrders(text, merged)
		_ = marshalSettingsMapIndent([]string{"terminal"}, merged,
			nestedKeyOrder(text, merged, nil), arrayFieldOrders(text, merged))
	})
}
