//go:build race

package tui

// markdownLexerAllocationBudget is the ceiling for the lexer's inline allocation test
// under the race detector, which needs its own number: the race runtime's
// instrumentation defeats part of the escape analysis the batching relies on, so the same
// lexer allocates about 2.3x more objects. Measured on CI's amd64 runner (the only place
// this build runs, since CI runs `go test -race -count=2 ./...` and nothing else): 2,404
// and 2,413 objects, against about 1,050 for the same fixture without the detector.
//
// A shared budget would have to be the looser of the two and would then stop catching the
// regression both exist for, so the budget is per build. 3,000 keeps that guard alive in
// CI with room for the noise: a lexer that went back to one object per plain-text run
// allocated 1,909 objects before the fix, which scales to roughly 4,400 under the same
// instrumentation.
const markdownLexerAllocationBudget = 3000
