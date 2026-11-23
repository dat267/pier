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

