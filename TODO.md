# Reproduced, unresolved findings

Measured on 24 KiB samples grown from `internal/highlight/testdata/sample.<ext>`
(the first 73 bytes become the body for HTTP), against highlight.js 10.7.3 as
pinned by pi v1.6.0 (`faac5858`), both sides through a tree-emitting harness and
three timed passes per language, best of three. Median ratio over 191 languages
is 0.38x, 176 are faster than the reference and 182 are within 1.25x. The nine
below are what is left, worst first.

## R is still quadratic, and it is why `http` is 4.65x (open)

The single biggest one. 1 KiB 7.7 ms, 2 KiB 29 ms, 4 KiB 116 ms against a linear
reference (0.79, 2.09, 2.12). `http` is 6335 ms against 1363 and 5394 ms of that
is R, because HTTP's body is a `subLanguage: []` mode that auto-detects against
every language and R is the slowest candidate.

The mechanism is located. `resumableMultiRegex.exec` resumes at `regexIndex` 11,
`getMatcher(11)` builds `rules[11:]` of R's root rule set, and the engine resumes
there once per lexeme: 213 times on 1 KiB, 390 on 2 KiB, 1577 on 4 KiB. Every one
of those resumes takes the `result.index != lastIndex` branch and runs the full
matcher from `lastIndex+1`, so the partial matcher's work is wasted: measured, it
matched exactly at `lastIndex` zero times in 213 resumes. The tail rule set's
first-byte table admits 68% of bytes (it carries
`[a-zA-Z][a-zA-Z_0-9]*\s+<-\s+`), so `execDispatch` makes about 610 iterations per
resume at about 90 ns each, and one resume costs 57 us where V8's single native
`g`-regex call costs 14 us for the same tail.

Five fixes were written, measured and reverted; see D527 for each. The closest was
a required-literal mask, which took R to 0.44 ms and linear to 32 KiB but
regressed 188 of 191 languages, because dropping a rule per candidate put
`strings.Contains` at 90% of a 24 KiB R profile and because skipping a matcher
whose rules are all ruled out stops the engine from entering the sublanguage modes
latex needs.

So what is needed is a dispatch that settles a broad rule set in one pass rather
than one candidate at a time, designed with the sublanguage path in view. Go's own
joined regexp is no help: 139 us for that four-rule alternation, since it has no
common literal prefix for Go to scan. Constraint to preserve: counting
walk-settled rejections toward `maxRejectedCandidates` breaks D351's comment
guard, pinned by `TestRejectedCandidateRunStopsAtBudget`.

## autohotkey is 4.06x and cannot dispatch (open)

21.67 ms against 5.34. `dispatch=false` with zero rules lacking a first byte, so
the cause is the 240-byte selectivity cap: a manual rule's `firsts` cover 252 of
256 bytes (`[^\n";]` really is that broad), the union then exceeds the cap and the
whole table is refused. Only 18,980 walk steps for the whole file, so the time is
Go's regexp, as for `basic`.

Without the manual rules the union is 127 of 256, so the regex rules alone would
dispatch; the 252-byte manual `firsts` are what push it to 255. Excluding such a
rule from the union and trying it at every candidate was written and reverted: it
changes which manual rule wins, and `TestAutoHotkeyTitleBacktracking` shows the
title rule must be reached in byte-indexed order. A fix needs a selective table
over the regex rules while keeping manual rules reachable in order.

## pgsql is 3.86x from one 70-rule alternation (open)

30.81 ms against 7.98. It dispatches; the cost is walk volume. One rule,
`:==|\W\s*\(\*|\{\{|[a-z]:\s*$|...`, is walked 16,347 times per 24 KiB. A 100-way
literal alternation that cannot match is rejected in 23 ns, so the cost is the
number of candidates rather than per-call depth.

## vbscript is 3.26x from one keyword alternation (open)

23.13 ms against 7.09. Same shape: `(?:lcase|month|vartype|instrrev|ubound|...)`
is walked 16,073 times, 1,635,513 walk steps total.

## basic is 2.61x and cannot dispatch (open)

28.99 ms against 11.13. `dispatch=false` with no rule lacking a first byte, and
only 25,431 walk steps, so the time is Go's regexp as for autohotkey. Here the
cause is not manual rules but the rules themselves: their first-byte sets union to
255 of 256 bytes, so nothing is selective enough for the 240-byte cap and the
table is refused.

## sas is 2.34x from one rule (open)

12.70 ms against 5.42. `[^%](?:abs|addr|airy|arcos|arsin|...)` is walked 15,221
times, 926,490 steps. The rule begins with a negated class, so its first-byte set
is broad.

## stata is 1.96x from one rule (open)

7.52 ms against 3.84. `^[ \t]*\*.*$` is walked 25,188 times, 341,248 steps. It is
line-anchored with an optional indent, the shape D528 taught the dispatch to
handle, so the remaining cost is the walk itself being run at every line start.

## armasm is 1.37x (open, low priority)

15.07 ms against 11.01. Already 9.80x to 1.54x by D528; the rest is its 770-byte
200-way mnemonic rule.

## java is 1.30x (open, low priority)

10.81 ms against 8.31. The generic-identifier rule
`[\u00C0-\u02B8a-zA-Z_$][...]*(<...>)?` dominates, as recorded under D524.
