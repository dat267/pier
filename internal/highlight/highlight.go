// Package highlight is a Go port of highlight.js 10.7.3 (the version the
// reference `pi` monorepo pins). It reproduces the reference's scope output
// (the nesting of `hljs-*` classes its HTML renderer emits) for the languages
// `pi` registers eagerly, so the TUI can map scopes to colors without the
// reference's JS engine.
//
// highlight.js compiles each mode's rules into one alternation and drives a
// scan with it; this port keeps that structure but compiles the alternations
// with Go's RE2 engine. 97% of the reference's compiled patterns are already
// RE2-compatible; the rest (lookaround, backreferences, `\uXXXX` escapes) are
// normalized or given an explicit equivalent here. pattern_test.go checks the
// whole set against a snapshot of the reference's compiled regexes.
//
// JavaScript and RE2 disagree about line boundaries and whitespace: JS ^/$
// recognize CR, LF, U+2028 and U+2029, and its \s is Unicode-aware. Patterns
// are normalized for the whitespace classes, and input containing CR, U+2028 or
// U+2029 is matched by a small Thompson VM over the same compiled program with
// JS assertions and dot semantics, so LF-only documents keep the RE2 path.
// Astral characters are stored as their two UTF-16 surrogate units for matching
// and restored to characters in the returned tree, matching JavaScript's
// code-unit indexing for dots, quantifiers, classes and offsets.
package highlight
