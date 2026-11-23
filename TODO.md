# Reproduced, unresolved findings

## One unreproduced `./coding` failure (2026-11-22)

One `go test -count=1 ./coding ./coding/interactive` run reported `FAIL
github.com/dat267/pier/coding 20.861s` while `./coding/interactive` passed. The
failure was not captured with its test name, and thirteen later runs of the same
packages, the same pair, and a `-race` subset all passed. It recurred once more
the same day, again with `./coding/interactive` green and again without the test
name (25 more runs passed, including five `-shuffle=on`).

`just test` now reprints every `--- FAIL:` block after the run, so the next
occurrence names its test without a rerun. If it names a test this repository
shares with `./coding/interactive`, check first whether it depends on wall-clock
timing or on process-global state (`leakcheck` and `just test-isolated` both hunt
for the second class).

## The interactive MCP sign-in has no caller (open, needs a scope decision)

`packages/mcp/src/oauth/sign-in.ts` `startMcpSignIn` runs `refreshBeforeSignIn`
under the store's refresh lock and, when that returns `REDIRECT`, starts the
browser step. Its only caller in the reference is
`extensions/mcp/oauth.ts:226`, which is the extension surface D41 puts out of
scope, so the missing piece is not one function: it is `startMcpSignIn`,
`completeMcpSignIn`, a loopback listener, the create-CIMD fallback and the `/mcp`
command that drives them. D259 already records the user-visible half (the
sign-in hint does not name a `/mcp` command).

The proactive half is ported (`mcp/mcp_federation.go`): `oauthAdapter.Token`
refreshes a stored access token within `mcpRefreshSkewMS` (30 s, the reference's
`REFRESH_SKEW_MS`) of its expiry, before the request goes out, through
`AuthorizeMcp` with `RefreshOnly: true`. A failure or a redirect falls through to
the old token, which the 401 path handles. Pinned by
`TestOAuthAdapterTokenRefreshesAnExpiredTokenProactively`.

So what remains is a scope call rather than a repair: port the interactive
sign-in as a pier command, or delete the dead `OnRedirect` seam and record the
omission. The pieces it would need:

1. A sign-in entry that reads the state, returns `AUTHORIZED` when the refresh
   alone settles it, and otherwise runs the browser step — keeping the pending
   `OAuthState` and code verifier the refresh left alone
   (`TestOAuthRefreshOnlyDoesNotStartAnAuthorization` already pins that half).
2. Real `OnRedirect` wiring in `coding/mcpruntime.go`, which is a no-op today,
   plus the command that drives it.
3. A cross-process refresh lock. The reference's is a file lock raising
   `McpRefreshLockBusyError`; pier's `writesMu` serializes writes inside one
   process, which is why the adapter's `inFlight` sharing is process-local.

## IDN validation is still approximate (open)

A 1,200-cell fuzz corpus diffed against the reference's `Is*` functions leaves 13
differences, and every one of them is the same root cause: the port hand-rolls the
IDN checks where the reference calls the shared
`packages/schema/src/formats/typebox/idna` implementation. A 62-label
differential against the reference's own compiled `IsIdnHostname` leaves 10
differences in two rules, and an earlier version of this entry had the PVALID
rule's direction backwards.

1. **CONTEXTO neighbours** (`label/unicode.ts:87`): U+00B7 needs `l` on both
   sides, U+0375 a following Greek character, U+05F3/U+05F4 a preceding Hebrew
   one, and U+200C/U+200D a preceding virama. The port has none of them, so it
   rejects `l·l` and `א׳`, both of which the reference accepts.
2. **PVALID categories** (`label/unicode.ts:84`): the reference enforces the RFC
   5892 permitted categories and the DISALLOWED exceptions; the port admits any
   `unicode.IsLetter`/`IsNumber` rune. The corpus found no rune the port admits
   and the reference forbids, so this rule is untested rather than wrong, and
   `::1`, `:`, `Zzé+59` and `:ａff60` are cases both accept.
3. **A-label length** (`label/unicode.ts:48`): a non-ASCII label whose punycode
   form plus `xn--` exceeds 63 octets is rejected. The port counts the label's own
   runes, so 63 CJK codepoints pass here and are rejected there. This one is
   confirmed and is the only case in the corpus where the port is more permissive.

`idn-email` is *not* in this group: it has its own regex and now matches exactly.

Closing this means porting the reference's label rule, not adopting
`golang.org/x/net/idna`. Measured: `idna.New(ValidateForRegistration(),
StrictDomainName(true))` agrees with the reference on `l·l` and on the CJK
A-label length but rejects `a_b.com`, `::1`, `:` and `Zzé+59` as well, so it is
not the oracle. It would also add a dependency: `golang.org/x/net` is only an
indirect graph entry, with no `go.sum` line, so `go get` fetches it (verified).

The reference's rule is `pattern/pattern.ts` (30 exported regexes, 116 lines),
`format/puny.ts` (167) and `format/bidi.ts` (127), about 410 lines of Unicode
tables to copy. Weighed against that: no tool schema in either tree declares the
`idn-hostname` format, so only the port's generic format dispatcher can reach it.
Left open as a deliberate trade rather than started and abandoned.

`iri-reference` is a separate, smaller item: the reference is
`!InvalidIriChars && !MalformedScheme && URL.canParse(value, "http://example.com")`
(`iri_reference.ts:41`), which is the WHATWG parser. The port applies a stricter
character rule, so it rejects `.@12Z|`, `}` and `{` which the reference accepts.
Porting it means deciding how much of `URL.canParse` to reproduce; `ai/absoluteURL`
already ports the `url` format's call, so the same approach applies here.

## A rejected-candidate run ends rule highlighting for the rest of the input (open)

Highlighting `strings.Repeat("a", 256) + " (1"` as `c` (255 as `javascript` or
`ruby`), the port renders the `1` uncoloured where the reference emits an
`hljs-number` span. Root cause found: `multiRegex.execDispatch` gives up after
`maxRejectedCandidates` (256) consecutive rejected first-byte candidates and
returns nil, and `engine.highlight` breaks its loop on a nil match, so no rule
matches anywhere after the run. Measured directly: with a 300-byte run, a number,
a string and a comment all render plain.

That is broader than D351 states. The row says the give-up "only affects input
past 256 rejected candidate bytes" and that "keywords still apply to the appended
text", which is true for keywords, but it omits that *rule* highlighting never
resumes. The original sighting was a rust highlight of lines 70-90 of
`pi/packages/env/daemon/src/output.rs` losing the `1` in
`counter.fetch_sub(1, Ordering::SeqCst);`; that slice reduces to this rule, which
is why reducing it by line never shrank it.

Why it is not fixed yet: resuming after the budget needs a fresh
`indexFirstByte` scan, and that scan is itself linear over the rejected run.
Measured on the D351 input (`"0x"` repeated, 32 KiB, javascript): baseline
315 ms, resume-with-one-refill 673 ms, resume-with-eight-refills 1.99 s, and an
unbounded refill 13.2 s. A 2.1x regression to recover one token's colour is not a
trade AGENTS.md allows without a D-row, and none of the variants is both correct
and at baseline. A real fix has to make the resume cheap, for example by having
`indexFirstByte` return the next candidate from the current scan instead of
re-scanning, or by making the submatcher failure O(1) per candidate.

No test pins this: the current behaviour is what D351 chose, and the reproduction
is a run length rather than a construct.

Re-examined while closing the other entries: the resume the fix needs is a
per-call budget rather than a scan budget, but `execDispatch` returns nil, so the
engine's loop ends (`engine.go`'s `if m == nil { break }`, which is also the
reference's contract). Making it resume means distinguishing "no match" from
"budget spent", threading that through `resumableMultiRegex.exec` and the engine,
and the submatcher failure stays O(tail) per candidate, so a resumed run pays the
same quadratic term again. D351's numbers already beat the reference on this
input, so the remaining bug is a missing-colour one. Left open deliberately: it
needs the regex layer to fail in O(1), not a caller-side patch, and D351's
measured trade is the better one until then.
