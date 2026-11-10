# Reproduced audit findings

## A477 `FooterTruncate` returns dots when the text already fits

- Severity: low (visible footer truncation at narrow widths).
- Reference (pi v1.5.0 `f9a26365`, `packages/tui/src/utils.ts:1186`
  `truncateToWidth`, which `footer.ts` calls): when the ellipsis does not fit
  (`ellipsisWidth >= maxWidth`), a text that already fits is returned as-is.
- Port: `FooterTruncate` (`coding/interactive/footer.go:126`) short-circuits
  `maxWidth <= 3` to `"..."[:maxWidth]` without the fits-check.
- Measured:

  | text | width | reference | port |
  |---|---|---|---|
  | `ab` | 2 | `ab` | `..` |
  | `a` | 2 | `a` | `..` |
  | `a` | 1 | `a` | `.` |
  | `` (empty) | 2 | `` | `..` |
  | `abc` | 3 | `abc` | `...` |

  Cases where the text does **not** fit agree (`日本語` at 2 -> `..` in both).
- Reachable: `FooterTruncate` renders the footer's cwd basename at 25 columns
  and the joined status line at `min(80, width)`, so a narrow terminal can hit
  the `<= 3` branch.
- Cause: the port re-implements `truncateToWidth` instead of porting its
  ellipsis branch; `tui.TruncateToWidth` has the correct logic but emits ANSI
  resets, which a footer string cannot carry.
- Fix: apply the reference's fits-check before the `maxWidth <= 3`
  short-circuit.
- Verification: a table test over the fitting and non-fitting cases at widths
  1-4, plus a CJK case.

## A478 `formatScopeGroups` sorts by byte order, not the reference's collation

- Severity: low (visible order in the loaded-resources listing).
- Reference (pi v1.5.0 `f9a26365`,
  `modes/interactive/interactive-mode.ts:1778-1787`): sorts the group's paths,
  the package sources and each package's paths with `a.localeCompare(b)`.
- Port: `formatScopeGroups` (`coding/interactive/loadedresources.go:129-138`)
  uses `sort.Slice` with `<` on the path and `sort.Strings` on the sources, so
  ordering is UTF-8 byte order.
- Measured, five of six corpora differ:

  | input | reference (`localeCompare`) | port (byte order) |
  |---|---|---|
  | `a/b.md`, `a/B.md`, `a/c.md` | `a/b.md`, `a/B.md`, `a/c.md` | `a/B.md`, `a/b.md`, `a/c.md` |
  | `Zebra.md`, `apple.md`, `Banana.md`, `cherry.md` | `apple.md`, `Banana.md`, `cherry.md`, `Zebra.md` | `Banana.md`, `Zebra.md`, `apple.md`, `cherry.md` |
  | `x/A.md`, `x/a.md`, `x/ä.md`, `x/z.md` | `x/a.md`, `x/A.md`, `x/ä.md`, `x/z.md` | `x/A.md`, `x/a.md`, `x/z.md`, `x/ä.md` |
  | `a/b/c.md`, `a/b-c.md`, `a/b_c.md`, `a/b.c.md`, `a/b1.md` | `a/b_c.md`, `a/b-c.md`, `a/b.c.md`, `a/b/c.md`, `a/b1.md` | `a/b-c.md`, `a/b.c.md`, `a/b/c.md`, `a/b1.md`, `a/b_c.md` |
  | `README.md`, `agents.md`, `Agent.md`, `zzz.md`, `_x.md`, `-y.md` | `_x.md`, `-y.md`, `Agent.md`, `agents.md`, `README.md`, `zzz.md` | `-y.md`, `Agent.md`, `README.md`, `_x.md`, `agents.md`, `zzz.md` |

  The sixth corpus (package sources) matches by coincidence.
- Same defect in the adjacent `formatCompactListSorted`
  (`loadedresources.go:172`): the reference's `formatCompactList`
  (`interactive-mode.ts:1888-1893`) sorts labels with `localeCompare` too, so
  `Zebra, apple, Banana, cherry` renders `Banana, Zebra, apple, cherry` instead
  of `apple, Banana, cherry, Zebra`. `{sort:false}` callers (context files) are
  unaffected.
- Reachable: `formatScopeGroups` renders the expanded `/loaded` listing, called
  from `coding/interactive/loadedresources.go:314` and `:337`.
- Cause: the port already chose locale-neutral root collation for this exact
  question (D257) in `modelselector.go`, `footer.go` and `listmodels.go`, but
  this call site was missed. A host with mixed-case resource names therefore
  lists them in an order the reference never produces.
- Fix: sort with `collate.New(language.Und)`, matching the three existing
  sites.
- Verification: root collation matches `localeCompare` on all six corpora
  (0 diffs); a table test pins the ordering.

## A479 `cacheShare` rounds halves to even where the module formats JS-style

- Severity: low (a 0.1 point difference in the working indicator's cache
  share).
- Context: `cacheShare` is pier-specific (D416), so there is no upstream string
  to match. The question is internal consistency: the same one-decimal display
  is formatted by `formatFixed` -> `ai.JSNumberToFixed` in `footer.go`, while
  `statusindicator.go:171` uses `strconv.FormatFloat(share, 'f', 1, 64)`.
- Go's `FormatFloat` rounds halfway cases to even; JS `toFixed` rounds them
  away from zero (upstream `footer.ts:162` and `:27-29` rely on that).
- Measured over `cacheRead` 1..3 and `in` 1..2000, 12 differ; 9 are the
  halfway cases (the other 3 are the deliberate `> 100` clamp):

  | in | cacheRead | port | JS `toFixed(1)` |
  |---|---|---|---|
  | 16 | 1 | `CH6.2%` | `CH6.3%` |
  | 32 | 2 | `CH6.2%` | `CH6.3%` |
  | 80 | 1 | `CH1.2%` | `CH1.3%` |
  | 400 | 1 | `CH0.2%` | `CH0.3%` |
  | 1600 | 100 | `CH6.2%` | `CH6.3%` |

  Reachable whenever `2000*cacheRead/in` is an odd integer, e.g. `in` 1600 with
  `cacheRead` 100, a plausible real pair.
- Cause: a second rounding rule for a display the module formats one way
  everywhere else.
- Fix: format with `ai.JSNumberToFixed`, as `formatFixed` does.
- Verification: a table test over the halfway ratios plus the clamp.

## A480 a tool path is never a clickable hyperlink

- Severity: low (lost terminal affordance; the path renders identically with
  hyperlinks on and off).
- Reference (pi v1.5.0 `f9a26365`, `core/tools/render-utils.ts:170-180`):
  `renderToolPath` returns
  `linkPath(theme.fg("accent", shortenPath(value)), value, cwd)`, and
  `linkPath` (`:15-19`) returns the styled text unchanged when
  `getCapabilities().hyperlinks` is off, else wraps it in a `pathToFileURL`
  hyperlink built from `resolvePath(rawPath, cwd)`.
- Port: `RenderToolPath` (`coding/interactive/toolresult.go:100-113`) takes
  `cwd` but never uses it, and returns the styled path with the comment
  "Hyperlinks need the path-to-file-URL conversion; with capabilities off this
  is the plain styled path". Measured:

  | capabilities | port output |
  |---|---|
  | `Hyperlinks: true` | `\x1b[38;5;215m/home/dat/repos/pier/coding/paths.go\x1b[39m` |
  | `Hyperlinks: false` | identical |

  So a terminal that supports OSC 8 gets no link, while markdown links in the
  same transcript do get one (`tui/markdown.go:669`,
  `internal/markdown/layout.go:55`).
- Reachable: the `read`, `write`, `edit` and `ls` renderers all call
  `RenderToolPath`.
- Cause: the port implemented the capability-off branch only and dropped the
  URL construction.
- Fix: resolve the path against `cwd`, convert to a `file://` URL, and wrap the
  styled text in OSC 8 when `Hyperlinks` is on.
- Verification: a test asserting the link is present with capabilities on, the
  plain styled path with them off, and that a relative path resolves against
  `cwd`.

## A482 one `just check` run failed and never reproduced

- Severity: low (a single unexplained gate failure; if real, it is a flaky test).
- Observed: `just check` at 13:16 printed `FAIL` and
  `error: recipe 'test' failed on line 67 with exit code 1` with no failing
  package line captured in the truncated tail. The immediately following run,
  and every run since, passed.
- Not reproduced after: 3 further `just check` runs, 6 `go test -count=1 ./...`
  runs (19 packages ok each), 8 `./coding/interactive`, 6 `./tui ./coding ./mcp
  ./agent`, a `go clean -testcache` run, and 3 `./...` runs under 6 background
  CPU burners. The tight-deadline tests (`agent/abort_settlement_test.go:78` 20ms,
  `ai/mistral_timeout_zero_test.go:47` 20ms, `ai/*_test.go` 25ms) were the
  prime suspects by inspection and did not fire under load.
- Unconfirmed: no red-capable seam exists for "a test that failed once", so per
  the red-first rule this stays unpatched. Re-open with the failing package and
  test name if it recurs.
