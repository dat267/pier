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
