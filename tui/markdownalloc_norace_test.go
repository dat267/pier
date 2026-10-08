//go:build !race

package tui

// markdownLexerAllocationBudget is the ceiling for the lexer's inline allocation test in
// an ordinary build. The pre-fix lexer allocated about 1,909 objects for this fixture;
// the batching fix brought it to about 1,050, so 1,150 keeps headroom without allowing
// the per-run costs back.
const markdownLexerAllocationBudget = 1150
