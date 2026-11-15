package highlight

import "testing"

// TestJuliaReplHasNoJldoctestAlias pins pi v1.5.2 (b4afdddf) highlight.js 10.7.3
// lib/languages/julia-repl.js: the `aliases: ['jldoctest']` entry sits on the
// `^julia>` mode object, where the compiler ignores it, so the registered
// language has no aliases and `jldoctest` resolves to nothing. The port lifted it
// to the language definition, so pier accepted a name the reference rejects.
func TestJuliaReplHasNoJldoctestAlias(t *testing.T) {
	if !SupportsLanguage("julia-repl") {
		t.Fatal("julia-repl must stay registered")
	}
	if !SupportsLanguage("julia") {
		t.Fatal("julia must stay registered")
	}
	if SupportsLanguage("jldoctest") {
		t.Fatal("jldoctest is not a registered language in the reference")
	}
	if _, err := Highlight("x", "jldoctest"); err == nil {
		t.Fatal("highlighting with an unregistered alias must fail")
	}
}
