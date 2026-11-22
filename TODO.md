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

## Two IDN label rules are still approximate (open)

`validIDNLabel` (`ai/validation_pi.go`) ports the reference's IDN hostname check
more coarsely than it should. Two divergences remain after the ACE-prefix and
normalization fix:

1. **A-label length.** `label/unicode.ts:48` rejects a non-ASCII label whose
   punycode encoding plus the 4-byte `xn--` prefix exceeds 63 octets
   (`ExceedsMaxALabelLength`). The port counts the label's own runes, so it
   accepts `strings.Repeat("あ", 63)` where the reference rejects it (the
   punycode form is far longer).
2. **CONTEXTO neighbors.** `label/unicode.ts:87` rejects U+00B7 (middle dot)
   unless both neighbours are `l`, U+0375 unless followed by a Greek script
   character, U+05F3/U+05F4 unless preceded by Hebrew, and U+200C/U+200D unless
   preceded by a virama. The port has none of these, so it rejects `l·l` which
   the reference accepts (its `unicode.IsLetter` test fails the middle dot).

Both need the real RFC 5892 tables. `golang.org/x/net` is already an indirect
dependency at v0.61.0, and its `idna` package has the mappings, but the module has
no go.sum entry for it, so adding it is a dependency change. With it:

1. `go mod download golang.org/x/net` and add the go.sum entries.
2. Replace the hand-rolled body of `validIDNLabel` with
   `idna.New(idna.ValidateForRegistration(), idna.StrictDomainName(true)).ToASCII`
   and accept only when it succeeds, then diff against the reference over a
   corpus of IDN labels before trusting it.

Until then the two cases above are the known difference.

## A `just check` run can report a spurious `[build failed]` (resolved)

Reported three times this session: `FAIL .../coding [build failed]` (sometimes also
`coding/interactive` and `mcp/oauth`) while `go build ./...`, `go vet ./coding` on
three platforms and `go test -count=1 ./...` all passed immediately afterwards.

Cause found: `/tmp` is a tmpfs and had reached 78% (1.8 GB free of 7.7 GB), almost
all of it this session's own scratch (a 973 MB catalog checkout, a 332 MB session
export, a 32 MB windows test binary). Go links test binaries through `/tmp`, so a
link failed under pressure and the package line is all `just test` prints. After
removing those files (3.4 GB free) three consecutive `just check` runs were clean,
and a deliberate overlap of two `go test` runs did not reproduce it, so it was
never contention.

Keeping scratch under a few hundred MB avoids it. The capture command, if it ever
recurs for another reason:

```sh
go test -count=1 ./coding 2>&1 | tee /tmp/coding-build.log
grep -A10 'build failed\|cannot\|undefined' /tmp/coding-build.log
```
