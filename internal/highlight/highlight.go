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
package highlight
