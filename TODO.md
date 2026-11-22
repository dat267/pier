# Reproduced, unresolved findings

## The first-run setup dialog is reachable only from its golden test (open)

Upstream `main.ts:719` runs `showFirstTimeSetup(startupSettingsManager)` when
`shouldRunFirstTimeSetup()` (interactive, no `--help`, no `--list-models`). It is
how a new install picks a theme and opts in or out of analytics, and it writes
`enableAnalytics`/`trackingId` through the settings manager.

pier has no `shouldRunFirstTimeSetup` and no `showFirstTimeSetup`: `cmd/main.go`
never calls them, so `NewFirstTimeSetupComponent` (`coding/interactive/eastereggs.go`)
has no production caller and the only reach is `eastereggs_test.go`'s golden.

This is consistent with two recorded omissions rather than an oversight: D236
drops upstream's `telemetry` package entirely and states that `ai`'s optional
`telemetryContext` is never set, and the analytics opt-in exists only to feed
that package. Implementing the dialog without an analytics sink would add a
first-run theme prompt the port has no code to honour.

What a future change needs, if the port ever gains an analytics sink:

1. The settings half already exists: `SetEnableAnalytics` writes
   `enableAnalytics` and, on first opt-in, `trackingId`
   (`coding/settingsmanager.go`), matching `settings-manager.ts`.
2. Port `shouldRunFirstTimeSetup`'s settings-file check and
   `showFirstTimeSetup`'s own startup TUI (the pattern `ShowStartupSelector`
   already uses), then call it from `cmd/main.go` before the app's TUI.
3. The component's `DetectedTheme` option and the "Detected system appearance:"
   line come from the legacy commit the golden pins
   (`d1230ea2`, `coding/interactive/testdata/golden-reference.mjs`); the pin
   (`faac5858`) replaced them with a System/Dark/Light list. Regenerate the
   golden against the legacy commit if the options change, or the check fails.

## One unreproduced `./coding` failure (2026-11-22)

One `go test -count=1 ./coding ./coding/interactive` run reported `FAIL
github.com/dat267/pier/coding 20.861s` while `./coding/interactive` passed. The
failure was not captured with its test name, and thirteen later runs of the same
packages, the same pair, and a `-race` subset all passed. It recurred once more the
same day, again with `./coding/interactive` green and again without the test name
(25 more runs passed, including five `-shuffle=on`).

The next occurrence needs the failing test named, so run without a filter and
keep the whole `--- FAIL:` block:

```sh
go test -count=1 ./coding 2>&1 | grep -B2 -A15 -- '--- FAIL'
```

If it names a test this repository shares with `./coding/interactive`, check
first whether it depends on wall-clock timing or on process-global state
(`leakcheck` and `just test-isolated` both hunt for the second class).

## The daxnuts easter egg has no production caller (open)

The legacy commit the interactive fixtures pin (`d1230ea2`) hides a daxnuts easter
egg for the OpenCode Kimi K2.5 model: `checkDaxnutsEasterEgg(model)` runs after
`session.setModel`, after the selector's `selectModel`, and after a login that
selected a model (three call sites). pier ported the component and the predicate
(`CommandWiring.CheckDaxnutsEasterEgg`) but wired it into nothing:
`ModelWiring.OnModelSelected`, which is where the model-selection hook now lives
(`coding/interactive/interactivemode_models.go:607`), runs only
`MaybeWarnAboutAnthropicSubscriptionAuth`. So the egg is dead except in
`interactivemode_commands_test.go`.

Repair is one call in each of the two hooks the legacy sites funnel into:
`newModelWiring`'s `OnModelSelected` and the login's `OnAuthenticated` (which is
wired to an empty function at `interactivemode_auth.go:894`). Both would call
`app.commands.CheckDaxnutsEasterEgg(model.Provider, model.ID, app.ui)`.

Both hooks are where the three legacy call sites converge: the selector and the
`/model` command reach `OnModelSelected`, and a login that selected a model
reaches `OnAuthenticated`.

Note that the pin (`faac5858`) removed the easter egg entirely, so this is
completing a legacy surface rather than keeping parity with the pin; leave it
unwired if the legacy easter eggs are being retired instead.

## The proactive token refresh has no caller (open)

Upstream `packages/mcp/src/oauth/auth-provider.ts` has two paths: `onUnauthorized`
(the reactive 401 path, which pier ports as `oauthAdapter.runRefresh`) and
`refreshStoredTokens` (`auth-provider.ts:107-130`), a proactive refresh that runs
before a request when the stored access token is expired. Only the second passes
`refreshOnly: true`, which makes `runFlow` return `REDIRECT` instead of
registering a client (`flow.ts:396`) or preparing an authorization (`flow.ts:453`).

pier ports the option (`FlowOptions.RefreshOnly`, with both early returns, pinned by
`TestOAuthRefreshOnlyRedirectsWithoutRegisteringWhenNoClientIsStored` and
`TestOAuthRefreshOnlyDoesNotStartAnAuthorization`) but has no caller for it: there
is no proactive refresh, so an expired access token is discovered only by the 401.
That is a deliberate difference in shape, not a bug in the ported option.

Wiring it needs the pre-request half of the reference:

1. A check of `state.TokensExpireAt` (already stored by `SaveTokens`) before a
   request in `StreamableHttpTransport`, calling `AuthorizeMcp` with
   `RefreshOnly: true` under the store's refresh lock.
2. Treating `ResultRedirect` from that call as `McpOAuthAuthorizationRequiredError`,
   which `runRefresh` already does for the 401 path.
3. A test that an expired token with no stored client redirects instead of
   registering, and one that a pending sign-in keeps its state and verifier.

## IDN validation is still approximate in four places (open)

A 1,200-cell fuzz corpus diffed against the reference's `Is*` functions leaves 13
differences, and every one of them is the same root cause: the port hand-rolls the
IDN checks where the reference calls the shared `packages/schema/src/formats/typebox/idna`
implementation. Grouped by rule:

1. **A-label length** (`label/unicode.ts:48`): a non-ASCII label whose punycode form
   plus `xn--` exceeds 63 octets is rejected. The port counts the label's own runes,
   so `strings.Repeat("あ", 63)` and `xn--123e4567123e456759` pass as `idn-hostname`
   and as `hostname` (which delegates to the same check, `hostname.ts:37`).
2. **CONTEXTO neighbours** (`label/unicode.ts:87`): U+00B7 needs `l` on both sides,
   U+0375 a following Greek character, U+05F3/U+05F4 a preceding Hebrew one, and
   U+200C/U+200D a preceding virama. The port has none, so `l·l` is rejected.
3. **PVALID categories** (`label/unicode.ts:84`): the reference enforces the RFC 5892
   permitted categories and the DISALLOWED exceptions. The port accepts any
   `unicode.IsLetter`/`IsNumber` rune, so `idn-hostname` accepts `::1`, `:`,
   `Zzé+59` and `:ａff60`, which the reference rejects.

`idn-email` is *not* in this group: it has its own regex and now matches exactly.

All three need the RFC 5892 tables. `golang.org/x/net` is an indirect dependency at
v0.61.0 and its `idna` package has them, but the module has no go.sum entry for it,
so adding it is a dependency change. With it:

1. `go mod download golang.org/x/net`, add the go.sum entries.
2. Replace `validHostname` and `validIDNLabel` with
   `idna.New(idna.ValidateForRegistration(), idna.StrictDomainName(true)).ToASCII`,
   accepting only on success. This closes all three rules at once, and fixes both
   formats because `hostname` delegates to the same check upstream.
3. Diff a corpus of a few hundred labels against the reference before trusting it,
   because `ValidateForRegistration` is stricter than the reference in places (it
   rejects labels the reference accepts, such as underscore-containing ones) and the
   two must be compared, not assumed equivalent.

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
