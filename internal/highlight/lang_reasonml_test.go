package highlight

import "testing"

// Reference: highlight.js 10.7.3 (pinned checkout), languages/reasonml.js.
// PARAMS_CONTENTS is built, referenced by PARAMS_MODE's typing mode, and the
// module-access mode is pushed into it only afterwards. A JavaScript array is a
// reference, so the typing mode sees that entry; a Go slice header copied into
// the literal does not, so the port has to install the appended slice.
// Without this the compiled typing scope differs from the reference wherever a
// module access appears inside a type annotation.
func TestReasonMLTypingContentsIncludeModuleAccess(t *testing.T) {
	compiled, err := getCompiledLanguage("reasonml")
	if err != nil {
		t.Fatal(err)
	}
	var typing *compiledMode
	var walk func(modes []*compiledMode)
	walk = func(modes []*compiledMode) {
		for _, mode := range modes {
			if mode == nil || typing != nil {
				continue
			}
			if mode.className == "typing" {
				typing = mode
				return
			}
			walk(mode.contains)
			if mode.starts != nil {
				walk([]*compiledMode{mode.starts})
			}
		}
	}
	walk(compiled.root.contains)
	if typing == nil {
		t.Fatal("reasonml typing scope not found")
	}
	classes := map[string]bool{}
	for _, mode := range typing.contains {
		classes[mode.className] = true
	}
	if !classes["module-access"] {
		t.Fatalf("typing contents = %v, want the module-access mode appended after construction", classes)
	}
	if !classes["module"] {
		t.Fatalf("typing contents = %v, want the original module mode retained", classes)
	}
}
